// Package workbenchsso implements the Workbench first-party login protocol.
// Protocol v1. Keep the adapter and its contract tests identical in each app.
package workbenchsso

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

const ProtocolVersion = "1"

type Identity struct {
	Subject string `json:"subject"`
	Name    string `json:"name"`
	Account string `json:"account"`
	Role    string `json:"role"`
}
type key struct{}

func FromRequest(r *http.Request) (Identity, bool) {
	v, ok := r.Context().Value(key{}).(Identity)
	return v, ok
}

type Client struct {
	Portal, Internal, Public, App, Secret string
	GatewaySecret                         string
	HTTP                                  *http.Client
}

func origin(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && !strings.Contains(raw, "#") && (u.Scheme == "http" || u.Scheme == "https")
}
func FromEnv(app string) (*Client, error) {
	p := os.Getenv("WORKBENCH_PORTAL_URL")
	secret := os.Getenv("WORKBENCH_CLIENT_SECRET")
	pub := os.Getenv("WORKBENCH_PUBLIC_URL")
	internal := os.Getenv("WORKBENCH_INTERNAL_URL")
	gateway := os.Getenv("WORKBENCH_GATEWAY_SECRET")
	if p == "" && secret == "" && pub == "" && internal == "" && gateway == "" {
		return nil, nil
	}
	if internal == "" {
		internal = p
	}
	public, e := url.Parse(pub)
	validPublic := e == nil && public.Hostname() != "" && public.User == nil && public.RawQuery == "" && !public.ForceQuery && !strings.Contains(pub, "#") && (public.Scheme == "http" || public.Scheme == "https") && (public.Path == "" || public.Path == "/apps/"+app)
	if validPublic && public.Path != "" && gateway == "" {
		return nil, errors.New("path-based applications require WORKBENCH_GATEWAY_SECRET")
	}
	if gateway != "" && (len(gateway) < 32 || !validPublic || public.Path != "/apps/"+app || public.Scheme+"://"+public.Host != p) {
		return nil, errors.New("gateway mode requires a >=32 character WORKBENCH_GATEWAY_SECRET and same-origin /apps/<id> public URL")
	}
	if !origin(p) || !validPublic || !origin(internal) || len(secret) < 32 {
		return nil, errors.New("SSO requires valid WORKBENCH_PORTAL_URL, WORKBENCH_PUBLIC_URL and WORKBENCH_CLIENT_SECRET (at least 32 characters)")
	}
	return &Client{Portal: p, Internal: internal, Public: pub, App: app, Secret: secret, GatewaySecret: gateway, HTTP: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func random() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func (c *Client) cookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: "wb_" + c.App + "_" + name, Value: value, Path: c.base() + "/", HttpOnly: true, Secure: strings.HasPrefix(c.Public, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func (c *Client) call(ctx context.Context, path string, body, out any) (int, error) {
	b, e := json.Marshal(body)
	if e != nil {
		return 0, e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", c.Internal+path, bytes.NewReader(b))
	if e != nil {
		return 0, e
	}
	public, _ := url.Parse(c.Portal)
	req.Host = public.Host
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workbench-SSO-Version", ProtocolVersion)
	req.SetBasicAuth(c.App, c.Secret)
	res, e := c.HTTP.Do(req)
	if e != nil {
		return 0, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return res.StatusCode, errors.New("统一认证服务拒绝请求")
	}
	return 200, json.NewDecoder(io.LimitReader(res.Body, 16<<10)).Decode(out)
}
func (c *Client) errResponse(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message, "message": message, "login_url": c.base() + "/auth/sso/login"})
}

// CSRF is bound to the validated app session; the bearer token stays HttpOnly.
func (c *Client) CSRF(r *http.Request) string {
	cookie, err := r.Cookie("wb_" + c.App + "_session")
	if err != nil || cookie.Value == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(c.Secret))
	mac.Write([]byte("workbench-csrf-v1\x00" + cookie.Value))
	return hex.EncodeToString(mac.Sum(nil))
}

func (c *Client) safeReturn(raw string) string {
	if raw == "" || len(raw) > 2048 || strings.Contains(raw, "\\") || strings.ContainsFunc(raw, unicode.IsControl) {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.Opaque != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || strings.Contains(u.Path, "\\") || strings.ContainsFunc(u.Path, unicode.IsControl) {
		return ""
	}
	clean := path.Clean(u.Path)
	if !strings.HasPrefix(clean+"/", c.base()+"/") || strings.HasPrefix(strings.TrimPrefix(clean, c.base()), "/auth/sso/") {
		return ""
	}
	return raw
}
func (c *Client) Wrap(next http.Handler) http.Handler {
	if c == nil {
		return next
	}
	var total, failures, duration, active atomic.Int64
	started := time.Now().UnixNano()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		public, _ := url.Parse(c.Public)
		if c.GatewaySecret != "" {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			ip := net.ParseIP(host)
			if err != nil || ip == nil || !ip.IsLoopback() || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Workbench-Gateway")), []byte(c.GatewaySecret)) != 1 {
				c.errResponse(w, 403, "该应用仅允许通过统一工作台访问")
				return
			}
		}
		if r.Host != public.Host {
			c.errResponse(w, 403, "应用访问地址不匹配")
			return
		}
		if r.URL.Path == "/internal/workbench/metrics" && r.Method == "GET" {
			app, secret, ok := r.BasicAuth()
			if !ok || app != c.App || subtle.ConstantTimeCompare([]byte(secret), []byte(c.Secret)) != 1 {
				c.errResponse(w, 401, "监控凭据错误")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]int64{"started": started, "requests": total.Load(), "errors": failures.Load(), "duration_us": duration.Load(), "active": active.Load()})
			return
		}
		if (r.URL.Path == "/health" || r.URL.Path == "/healthz") && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"ok":true}`)
			return
		}
		if r.URL.Path == "/auth/sso/info" && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"enabled": true, "version": ProtocolVersion, "portal_url": c.Portal, "app": c.App, "base_path": c.base()})
			return
		}
		if r.URL.Path == "/auth/sso/login" && r.Method == "GET" {
			state := random()
			c.cookie(w, "state", state, 300)
			c.cookie(w, "return", url.QueryEscape(c.safeReturn(r.URL.Query().Get("return"))), 300)
			workspace := ""
			if c.App == "assets" && (r.URL.Query().Get("workspace") == "finance" || r.URL.Query().Get("workspace") == "assets") {
				workspace = r.URL.Query().Get("workspace")
			}
			c.cookie(w, "workspace", workspace, 300)
			http.Redirect(w, r, c.Portal+"/sso/authorize?app="+url.QueryEscape(c.App)+"&state="+state, http.StatusSeeOther)
			return
		}
		if r.URL.Path == "/auth/sso/callback" && r.Method == "GET" {
			state, e := r.Cookie("wb_" + c.App + "_state")
			if e != nil || len(state.Value) != 64 || subtle.ConstantTimeCompare([]byte(state.Value), []byte(r.URL.Query().Get("state"))) != 1 {
				c.errResponse(w, 403, "登录校验失败，请重新从工作台进入")
				return
			}
			c.cookie(w, "state", "", -1)
			var result struct {
				Token    string   `json:"token"`
				Identity Identity `json:"identity"`
			}
			status, err := c.call(r.Context(), "/api/sso/token", map[string]string{"code": r.URL.Query().Get("code")}, &result)
			e = err
			if e != nil || len(result.Token) != 64 {
				if status == 0 || status >= 500 {
					c.errResponse(w, 503, "统一认证服务暂不可用，请稍后重试")
					return
				}
				c.errResponse(w, 502, "统一登录失败，请重新尝试")
				return
			}
			c.cookie(w, "session", result.Token, 8*3600)
			entry := c.base() + "/"
			if workspace, err := r.Cookie("wb_" + c.App + "_workspace"); err == nil && c.App == "assets" && (workspace.Value == "finance" || workspace.Value == "assets") {
				entry += "?workspace=" + workspace.Value
			}
			c.cookie(w, "workspace", "", -1)
			if target, err := r.Cookie("wb_" + c.App + "_return"); err == nil {
				decoded, _ := url.QueryUnescape(target.Value)
				if next := c.safeReturn(decoded); next != "" {
					entry = next
				}
			}
			c.cookie(w, "return", "", -1)
			http.Redirect(w, r, entry, http.StatusSeeOther)
			return
		}
		cookie, e := r.Cookie("wb_" + c.App + "_session")
		var identity Identity
		status := 401
		if e == nil {
			status, e = c.call(r.Context(), "/api/sso/introspect", map[string]string{"token": cookie.Value}, &identity)
		}
		if e != nil || identity.Subject == "" || identity.Account == "" || (identity.Role != "admin" && identity.Role != "user") {
			if status == 0 || status >= 500 {
				c.errResponse(w, 503, "统一认证服务暂不可用，请稍后重试")
				return
			}
			c.cookie(w, "session", "", -1)
			if r.Method == "GET" && !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/auth/sso/session" {
				entry := c.base() + "/auth/sso/login"
				if c.App == "assets" && (r.URL.Query().Get("workspace") == "finance" || r.URL.Query().Get("workspace") == "assets") {
					entry += "?workspace=" + r.URL.Query().Get("workspace")
				}
				if target := c.safeReturn(c.base() + r.URL.RequestURI()); target != "" && target != c.base()+"/" {
					separator := "?"
					if strings.Contains(entry, "?") {
						separator = "&"
					}
					entry += separator + "return=" + url.QueryEscape(target)
				}
				http.Redirect(w, r, entry, http.StatusSeeOther)
			} else {
				c.errResponse(w, 401, "统一会话已失效，请重新进入工作台")
			}
			return
		}
		if r.URL.Path == "/auth/sso/session" && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"username": identity.Subject, "account": identity.Account, "name": identity.Name, "role": identity.Role}, "csrf": c.CSRF(r), "portal_url": c.Portal, "version": ProtocolVersion})
			return
		}
		if r.URL.Path == "/api/logout" && r.Method == "POST" {
			if r.Header.Get("Origin") != public.Scheme+"://"+public.Host || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(c.CSRF(r))) != 1 {
				c.errResponse(w, 403, "请求来源不匹配")
				return
			}
			var result map[string]bool
			if _, e := c.call(r.Context(), "/api/sso/logout", map[string]string{"token": cookie.Value}, &result); e != nil {
				c.errResponse(w, 503, "退出失败，请稍后重试或在工作台全局退出")
				return
			}
			c.cookie(w, "session", "", -1)
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"ok":true,"message":"已全局退出"}`)
			return
		}
		if c.GatewaySecret != "" && r.Method != "GET" && strings.HasPrefix(r.URL.Path, "/api/admin/users") {
			c.errResponse(w, 403, "账号与状态由统一工作台管理")
			return
		}
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/password" || strings.HasPrefix(r.URL.Path, "/api/users") && r.Method != "GET" {
			c.errResponse(w, 403, "账号与密码由统一工作台管理")
			return
		}
		active.Add(1)
		start := time.Now()
		rec := &response{ResponseWriter: w, status: 200}
		defer func() {
			active.Add(-1)
			total.Add(1)
			duration.Add(time.Since(start).Microseconds())
			if rec.status >= 500 {
				failures.Add(1)
			}
		}()
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), key{}, identity)))
	})
}

type response struct {
	http.ResponseWriter
	status  int
	written bool
}

func (w *response) WriteHeader(code int) {
	if !w.written {
		w.status = code
		w.written = true
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *response) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (w *response) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (c *Client) base() string { u, _ := url.Parse(c.Public); return strings.TrimSuffix(u.Path, "/") }
func (c *Client) ValidateListen(addr string) error {
	if c == nil || c.GatewaySecret == "" {
		return nil
	}
	host, _, e := net.SplitHostPort(addr)
	ip := net.ParseIP(host)
	if e != nil || ip == nil || !ip.IsLoopback() {
		return errors.New("gateway-only applications must listen on a loopback IP")
	}
	return nil
}
