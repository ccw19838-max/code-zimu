package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github-catalog/internal/workbenchsso"
)

//go:embed web/*
var webFiles embed.FS

const sessionCookie = "github_catalog_session"

type Config struct {
	PublicURL     string
	NavigationURL string
	GitHubDemo    bool
	GitHubOrg     string
	ProfilesPath  string
	SSO           *workbenchsso.Client
}

func (c Config) validate() error {
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(c.PublicURL, "#") || u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("public-url must be an HTTP(S) origin without a path")
	}
	if c.NavigationURL != "" {
		u, err := url.Parse(c.NavigationURL)
		if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.Scheme != "http" && u.Scheme != "https" {
			return errors.New("navigation-url must be an HTTP(S) link without credentials")
		}
	}
	if c.GitHubDemo && c.GitHubOrg != "" {
		return errors.New("demo and github-org cannot be used together")
	}
	if c.SSO != nil && (c.SSO.App != "code-zimu" || c.SSO.Public != c.PublicURL || c.SSO.GatewaySecret != "") {
		return errors.New("code-zimu SSO requires its own public origin and code-zimu client ID, without gateway mode")
	}
	if c.GitHubOrg != "" && !githubOrgRE.MatchString(c.GitHubOrg) {
		return errors.New("github-org must be an organization login, not a URL")
	}
	if c.ProfilesPath != "" && (c.GitHubDemo || c.GitHubOrg == "") {
		return errors.New("profiles requires live github-org mode; demo uses embedded example profiles")
	}
	return nil
}

type Session struct {
	CSRF    string
	Expires time.Time
	Name    string
	Role    string
}

type loginAttempt struct {
	Count int
	Until time.Time
}

type App struct {
	cfg          Config
	host         string
	secure       bool
	passwordHash []byte
	passwordSalt string
	mu           sync.Mutex
	sessions     map[string]Session
	attempts     map[string]loginAttempt
	globalLogin  loginAttempt
	mux          *http.ServeMux
	github       *githubCatalog
	handler      http.Handler
}

func New(cfg Config, password string) (*App, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.SSO == nil && (cfg.GitHubOrg != "" && len(password) < 12 || password != "" && len(password) < 12) {
		return nil, errors.New("GITHUB_CATALOG_ADMIN_PASSWORD must have at least 12 characters; required for live mode")
	}
	if cfg.SSO != nil {
		cfg.NavigationURL = cfg.SSO.Portal
	}
	profiles, err := loadProfileFile(cfg.ProfilesPath)
	if err != nil {
		return nil, fmt.Errorf("profiles: %w", err)
	}
	u, _ := url.Parse(cfg.PublicURL)
	a := &App{cfg: cfg, host: u.Host, secure: u.Scheme == "https", sessions: map[string]Session{}, attempts: map[string]loginAttempt{}, mux: http.NewServeMux(), github: newGitHubCatalog(cfg)}
	if cfg.ProfilesPath != "" {
		a.github.profiles = profiles
	}
	if password != "" && cfg.SSO == nil {
		a.passwordSalt = randomToken()
		a.passwordHash = passwordKey(password, a.passwordSalt)
	}
	a.handler = cfg.SSO.Wrap(http.HandlerFunc(a.serveHTTP))
	a.mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) { jsonOut(w, a.sessionInfo(r)) })
	a.mux.HandleFunc("POST /api/login", a.login)
	a.mux.HandleFunc("POST /api/logout", a.requireAdmin(a.logout))
	a.githubRoutes()
	a.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonOut(w, map[string]string{"status": "ok"}) })
	a.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { problem(w, http.StatusNotFound, "API not found") })
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		return nil, fmt.Errorf("web assets: %w", err)
	}
	files := http.FileServer(http.FS(assets))
	a.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			problem(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		// Serve embedded files, never directories or an API fallback page.
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		info, err := fs.Stat(assets, name)
		// Team rules are intentional public content; project profile JSON remains
		// server-only and must never be exposed by the static file handler.
		if err != nil || info.IsDir() || strings.HasPrefix(r.URL.Path, "/api") || strings.EqualFold(path.Ext(name), ".json") && name != "team-rules.json" {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
	return a, nil
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}

func (a *App) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if r.Host != a.host {
		problem(w, http.StatusMisdirectedRequest, "invalid host")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != a.cfg.PublicURL {
		problem(w, http.StatusForbidden, "invalid origin")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("Origin") != a.cfg.PublicURL {
		problem(w, http.StatusForbidden, "origin required")
		return
	}
	a.mux.ServeHTTP(w, r)
}

type sessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	CSRF          string `json:"csrf"`
	Mode          string `json:"mode"`
	NavigationURL string `json:"navigation_url"`
	AuthMode      string `json:"auth_mode"`
	Name          string `json:"name,omitempty"`
	Role          string `json:"role,omitempty"`
}

func (a *App) sessionInfo(r *http.Request) sessionResponse {
	s, authenticated := a.session(r)
	mode := "disabled"
	if a.cfg.GitHubDemo {
		mode = "demo"
	} else if a.cfg.GitHubOrg != "" {
		mode = "live"
	}
	authMode := "local"
	if a.cfg.SSO != nil {
		authMode = "workbench"
	}
	return sessionResponse{Authenticated: authenticated, CSRF: s.CSRF, Mode: mode, NavigationURL: a.cfg.NavigationURL, AuthMode: authMode, Name: s.Name, Role: s.Role}
}

func (a *App) session(r *http.Request) (Session, bool) {
	if a.cfg.SSO != nil {
		identity, ok := workbenchsso.FromRequest(r)
		if !ok {
			return Session{}, false
		}
		return Session{CSRF: a.cfg.SSO.CSRF(r), Name: identity.Name, Role: identity.Role}, true
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return Session{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[cookie.Value]
	if !ok || !time.Now().Before(s.Expires) {
		delete(a.sessions, cookie.Value)
		return Session{}, false
	}
	if s.Role == "" {
		s.Role = "admin"
	}
	return s, true
}

func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.session(r)
		if !ok {
			problem(w, http.StatusUnauthorized, "请先登录项目目录管理员账号")
			return
		}
		if s.Role != "admin" {
			problem(w, http.StatusForbidden, "需要代码目录管理员权限")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRF)) != 1 {
			problem(w, http.StatusForbidden, "invalid CSRF token")
			return
		}
		next(w, r)
	}
}

func (a *App) allowLogin(remote string) bool {
	ip, _, err := net.SplitHostPort(remote)
	if err != nil {
		ip = remote
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for key, value := range a.attempts {
		if !now.Before(value.Until) {
			delete(a.attempts, key)
		}
	}
	if !now.Before(a.globalLogin.Until) {
		a.globalLogin = loginAttempt{Until: now.Add(15 * time.Minute)}
	}
	entry := a.attempts[ip]
	if entry.Until.IsZero() {
		entry.Until = now.Add(15 * time.Minute)
	}
	if entry.Count >= 10 || a.globalLogin.Count >= 100 || len(a.attempts) >= 512 {
		return false
	}
	entry.Count++
	a.globalLogin.Count++
	a.attempts[ip] = entry
	return true
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if len(a.passwordHash) == 0 {
		problem(w, http.StatusForbidden, "演示未配置管理员账号，可直接浏览示例目录")
		return
	}
	if !a.allowLogin(r.RemoteAddr) {
		w.Header().Set("Retry-After", "900")
		problem(w, http.StatusTooManyRequests, "登录尝试过多，请稍后重试")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		problem(w, http.StatusBadRequest, "invalid login request")
		return
	}
	key := passwordKey(body.Password, a.passwordSalt)
	if subtle.ConstantTimeCompare(key, a.passwordHash) != 1 || body.Username != "admin" {
		problem(w, http.StatusUnauthorized, "账号或密码错误")
		return
	}
	now := time.Now()
	id := randomToken()
	session := Session{CSRF: randomToken(), Expires: now.Add(8 * time.Hour), Name: "目录管理员", Role: "admin"}
	a.mu.Lock()
	for key, old := range a.sessions {
		if !now.Before(old.Expires) {
			delete(a.sessions, key)
		}
	}
	if old, err := r.Cookie(sessionCookie); err == nil {
		delete(a.sessions, old.Value)
	}
	if len(a.sessions) >= 256 {
		a.mu.Unlock()
		problem(w, http.StatusServiceUnavailable, "登录会话已满，请稍后重试")
		return
	}
	a.sessions[id] = session
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: id, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 60 * 60, Expires: session.Expires})
	jsonOut(w, sessionResponse{Authenticated: true, CSRF: session.CSRF, Mode: a.sessionInfo(r).Mode, NavigationURL: a.cfg.NavigationURL, AuthMode: "local", Name: session.Name, Role: session.Role})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(sessionCookie)
	a.mu.Lock()
	delete(a.sessions, cookie.Value)
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	jsonOut(w, a.sessionInfo(r))
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func passwordKey(password, salt string) []byte {
	key, err := pbkdf2.Key(sha256.New, password, []byte(salt), 210000, 32)
	if err != nil {
		panic(err)
	}
	return key
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return errors.New("JSON content type required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	if dec.Decode(new(any)) != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}

func jsonOut(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}

func problem(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
