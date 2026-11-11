package workbenchsso

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSessionAndGlobalLogoutAcrossOrigins(t *testing.T) {
	for _, gateway := range []bool{false, true} {
		t.Run(fmt.Sprint("gateway=", gateway), func(t *testing.T) {
			logout := 0
			portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				app, secret, _ := r.BasicAuth()
				if app != "assets" || secret != strings.Repeat("s", 32) || r.Header.Get("X-Workbench-SSO-Version") != ProtocolVersion {
					t.Error("missing versioned service authentication")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/sso/introspect":
					fmt.Fprint(w, `{"subject":"member","account":"existing_id","name":"成员","role":"user"}`)
				case "/api/sso/logout":
					logout++
					fmt.Fprint(w, `{"ok":true}`)
				default:
					t.Error("unexpected backchannel", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer portal.Close()
			c := &Client{Portal: portal.URL, Internal: portal.URL, Public: "https://app.test", App: "assets", Secret: strings.Repeat("s", 32), HTTP: portal.Client()}
			if gateway {
				c.Public += "/apps/assets"
				c.GatewaySecret = strings.Repeat("g", 32)
			}
			h := c.Wrap(http.NotFoundHandler())
			request := func(method, path, origin, csrf string, authenticated bool) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, "https://app.test"+path, nil)
				r.RemoteAddr = "127.0.0.1:8080"
				r.Header.Set("X-Workbench-Gateway", c.GatewaySecret)
				r.Header.Set("Origin", origin)
				r.Header.Set("X-CSRF-Token", csrf)
				if authenticated {
					r.AddCookie(&http.Cookie{Name: "wb_assets_session", Value: strings.Repeat("t", 64)})
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w
			}
			w := request("GET", "/auth/sso/session", "", "", false)
			if w.Code != 401 || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), c.base()+"/auth/sso/login") {
				t.Fatal("session API must return 401 with the correct login URL", w.Code, w.Body)
			}
			w = request("GET", "/auth/sso/session", "", "", true)
			var result struct {
				CSRF string   `json:"csrf"`
				User Identity `json:"user"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.CSRF) != 64 || result.CSRF == strings.Repeat("t", 64) || result.User.Account != "existing_id" || result.User.Role != "user" {
				t.Fatal("invalid session metadata", w.Code, w.Body)
			}
			for _, test := range []struct{ origin, csrf string }{{"https://evil.test", result.CSRF}, {"", result.CSRF}, {"https://app.test", ""}, {"https://app.test", "wrong"}} {
				if w := request("POST", "/api/logout", test.origin, test.csrf, true); w.Code != 403 {
					t.Fatal("unsafe logout accepted", w.Code)
				}
			}
			if logout != 0 {
				t.Fatal("rejected logout reached the portal")
			}
			w = request("POST", "/api/logout", "https://app.test", result.CSRF, true)
			if w.Code != 200 || logout != 1 {
				t.Fatal("global logout failed", w.Code, w.Body)
			}
			cookie := w.Result().Cookies()[0]
			if cookie.MaxAge != -1 || cookie.Path != c.base()+"/" || !cookie.HttpOnly || !cookie.Secure {
				t.Fatal("app session was not safely cleared", cookie)
			}
		})
	}
}

func TestReturnTargetsStayInsideApplication(t *testing.T) {
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"token":%q}`, strings.Repeat("t", 64))
	}))
	defer portal.Close()
	for _, base := range []string{"", "/apps/assets"} {
		c := &Client{Portal: portal.URL, Internal: portal.URL, Public: "http://app.test" + base, App: "assets", HTTP: portal.Client()}
		for _, target := range []string{base + "/?workspace=finance#/invoices", base + "/project/123?view=files#version", "https://evil.test", "//evil.test", "/%2f/evil.test", "/%5cevil.test", "/%09/evil.test", "/apps/assets/../../evil", base + "/auth/sso/login", "/apps/xrplan/"} {
			t.Run(base+":"+target, func(t *testing.T) {
				h := c.Wrap(http.NotFoundHandler())
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("GET", "http://app.test/auth/sso/login?return="+url.QueryEscape(target), nil))
				cookies := w.Result().Cookies()
				state := ""
				for _, cookie := range cookies {
					if cookie.Name == "wb_assets_state" {
						state = cookie.Value
					}
				}
				r := httptest.NewRequest("GET", "http://app.test/auth/sso/callback?code=one&state="+state, nil)
				for _, cookie := range cookies {
					r.AddCookie(cookie)
				}
				w = httptest.NewRecorder()
				h.ServeHTTP(w, r)
				expected := base + "/"
				if strings.Contains(target, "workspace=finance") || strings.Contains(target, "project/123") {
					expected = target
				}
				// With an origin-only app, any ordinary local path is inside it.
				if base == "" && (target == "/apps/xrplan/") {
					expected = target
				}
				if base == "" && target == "/apps/assets/../../evil" {
					expected = "/evil"
				}
				if w.Code != 303 || w.Header().Get("Location") != expected {
					t.Fatalf("return escaped or was lost: %d %q want %q", w.Code, w.Header().Get("Location"), expected)
				}
			})
		}
	}
}

func TestBackchannelDoesNotFollowCredentialRedirect(t *testing.T) {
	landed := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { landed = true }))
	defer target.Close()
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer portal.Close()
	for _, key := range []string{"WORKBENCH_PORTAL_URL", "WORKBENCH_PUBLIC_URL", "WORKBENCH_INTERNAL_URL", "WORKBENCH_CLIENT_SECRET", "WORKBENCH_GATEWAY_SECRET"} {
		t.Setenv(key, "")
	}
	t.Setenv("WORKBENCH_PORTAL_URL", portal.URL)
	t.Setenv("WORKBENCH_PUBLIC_URL", "http://app.test")
	t.Setenv("WORKBENCH_CLIENT_SECRET", strings.Repeat("s", 32))
	c, err := FromEnv("assets")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://app.test/api/me", nil)
	r.AddCookie(&http.Cookie{Name: "wb_assets_session", Value: strings.Repeat("t", 64)})
	w := httptest.NewRecorder()
	c.Wrap(http.NotFoundHandler()).ServeHTTP(w, r)
	if landed || w.Code != 401 {
		t.Fatal("credential redirect was followed or accepted", landed, w.Code)
	}
}

func TestSSORejectsNonOriginConfiguration(t *testing.T) {
	for _, key := range []string{"WORKBENCH_PORTAL_URL", "WORKBENCH_PUBLIC_URL", "WORKBENCH_INTERNAL_URL", "WORKBENCH_CLIENT_SECRET", "WORKBENCH_GATEWAY_SECRET"} {
		t.Setenv(key, "")
	}
	t.Setenv("WORKBENCH_CLIENT_SECRET", strings.Repeat("s", 32))
	for _, field := range []string{"WORKBENCH_PORTAL_URL", "WORKBENCH_PUBLIC_URL", "WORKBENCH_INTERNAL_URL"} {
		for _, invalid := range []string{"http://app.test?", "http://app.test#", "http://:80", "https://user:secret@app.test", "https://app.test/path"} {
			t.Setenv("WORKBENCH_PORTAL_URL", "http://portal.test")
			t.Setenv("WORKBENCH_PUBLIC_URL", "http://app.test")
			t.Setenv("WORKBENCH_INTERNAL_URL", "http://portal-internal.test")
			t.Setenv(field, invalid)
			if _, err := FromEnv("assets"); err == nil {
				t.Fatal("non-origin accepted", field, invalid)
			}
		}
	}
}
