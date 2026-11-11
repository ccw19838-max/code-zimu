package workbenchsso

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFailClosedAndNoLocalBypass(t *testing.T) {
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "revoked", 401) }))
	defer portal.Close()
	c := &Client{Portal: portal.URL, Internal: portal.URL, Public: "http://app.test", App: "assets", Secret: strings.Repeat("s", 32), HTTP: &http.Client{Timeout: time.Second}}
	called := false
	h := c.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	r := httptest.NewRequest("GET", "http://app.test/api/me", nil)
	r.AddCookie(&http.Cookie{Name: "lab_session", Value: "local-session"})
	r.AddCookie(&http.Cookie{Name: "wb_assets_session", Value: "revoked"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || called {
		t.Fatal("local session bypassed portal", w.Code)
	}
	portal.Close()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 || called {
		t.Fatal("offline auth did not fail closed", w.Code)
	}
}
func TestCallbackStateAndTrustedContext(t *testing.T) {
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, secret, _ := r.BasicAuth()
		if id != "assets" || secret != strings.Repeat("s", 32) {
			t.Error("client authentication missing")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"subject":"member","name":"Member","account":"lin","role":"user"}`))
	}))
	defer portal.Close()
	c := &Client{Portal: portal.URL, Internal: portal.URL, Public: "http://app.test", App: "assets", Secret: strings.Repeat("s", 32), HTTP: portal.Client()}
	called := false
	h := c.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := FromRequest(r)
		if !ok || u.Account != "lin" {
			t.Error("identity missing")
		}
		called = true
	}))
	r := httptest.NewRequest("GET", "http://app.test/auth/sso/callback?state=wrong", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || called {
		t.Fatal("invalid state accepted")
	}
	r = httptest.NewRequest("GET", "http://app.test/api/me", nil)
	r.Header.Set("X-Workbench-User", "admin")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if called || w.Code != 401 {
		t.Fatal("forged identity header accepted")
	}
	r.AddCookie(&http.Cookie{Name: "wb_assets_session", Value: strings.Repeat("t", 64)})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called || w.Code != 200 {
		t.Fatal("valid central identity rejected", w.Code)
	}
}
func TestPartialConfigurationRejected(t *testing.T) {
	for _, key := range []string{"WORKBENCH_PORTAL_URL", "WORKBENCH_PUBLIC_URL", "WORKBENCH_INTERNAL_URL", "WORKBENCH_CLIENT_SECRET"} {
		t.Setenv(key, "")
	}
	c, e := FromEnv("assets")
	if e != nil || c != nil {
		t.Fatal("standalone not preserved")
	}
	t.Setenv("WORKBENCH_PORTAL_URL", "http://portal.test")
	if _, e = FromEnv("assets"); e == nil {
		t.Fatal("partial SSO enabled silently")
	}
}

func TestRequestMetricsRequireServiceCredential(t *testing.T) {
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"subject":"member","account":"lin","name":"Member","role":"user"}`))
	}))
	defer portal.Close()
	c := &Client{Portal: portal.URL, Internal: portal.URL, Public: "http://app.test", App: "assets", Secret: strings.Repeat("s", 32), HTTP: portal.Client()}
	h := c.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "simulated business error", 503) }))
	r := httptest.NewRequest("GET", "http://app.test/api/work", nil)
	r.AddCookie(&http.Cookie{Name: "wb_assets_session", Value: strings.Repeat("t", 64)})
	h.ServeHTTP(httptest.NewRecorder(), r)
	r = httptest.NewRequest("GET", "http://app.test/internal/workbench/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("metrics exposed", w.Code)
	}
	r.SetBasicAuth("assets", c.Secret)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"requests":1`) || !strings.Contains(w.Body.String(), `"errors":1`) || !strings.Contains(w.Body.String(), `"active":0`) {
		t.Fatal("incorrect metrics", w.Code, w.Body)
	}
}

func TestOneTimeCallbackAndCookieAttributes(t *testing.T) {
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sso/token" {
			t.Error("unexpected request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"token":"` + strings.Repeat("t", 64) + `"}`))
	}))
	defer portal.Close()
	c := &Client{Portal: portal.URL, Internal: portal.URL, Public: "https://app.test", App: "assets", Secret: strings.Repeat("s", 32), HTTP: portal.Client()}
	h := c.Wrap(http.NotFoundHandler())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "https://app.test/auth/sso/login", nil))
	state := w.Result().Cookies()[0]
	if !state.HttpOnly || !state.Secure || state.SameSite != http.SameSiteLaxMode || len(state.Value) != 64 {
		t.Fatal("unsafe state cookie", state)
	}
	r := httptest.NewRequest("GET", "https://app.test/auth/sso/callback?code=one-time&state="+state.Value, nil)
	r.AddCookie(state)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatal("callback failed", w.Code)
	}
	cookies := map[string]*http.Cookie{}
	for _, cookie := range w.Result().Cookies() {
		cookies[cookie.Name] = cookie
	}
	stateCookie, sessionCookie := cookies["wb_assets_state"], cookies["wb_assets_session"]
	if stateCookie == nil || stateCookie.MaxAge != -1 || sessionCookie == nil || !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteLaxMode || sessionCookie.MaxAge != 8*3600 {
		t.Fatal("unsafe app cookie", cookies)
	}
}

func TestGatewayOnlyRejectsDirectAccessAndPublicListen(t *testing.T) {
	c := &Client{Public: "http://portal.test/apps/assets", App: "assets", GatewaySecret: strings.Repeat("g", 32)}
	if c.ValidateListen("0.0.0.0:8000") == nil {
		t.Fatal("gateway application allowed a public listener")
	}
	if e := c.ValidateListen("127.0.0.1:8000"); e != nil {
		t.Fatal(e)
	}
	h := c.Wrap(http.NotFoundHandler())
	r := httptest.NewRequest("GET", "http://portal.test/health", nil)
	r.RemoteAddr = "127.0.0.1:9999"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("direct backend accepted")
	}
	r.Header.Set("X-Workbench-Gateway", c.GatewaySecret)
	r.RemoteAddr = "192.168.1.20:9999"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("non-loopback gateway accepted")
	}
	r.RemoteAddr = "127.0.0.1:9999"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("trusted health probe rejected", w.Code)
	}
	cookieResponse := httptest.NewRecorder()
	c.cookie(cookieResponse, "state", "opaque", 300)
	last := cookieResponse.Result().Cookies()
	if last[len(last)-1].Path != "/apps/assets/" {
		t.Fatal("app cookie escaped prefix")
	}
}
func TestGatewayConfigurationCannotSilentlyFallBack(t *testing.T) {
	for _, k := range []string{"WORKBENCH_PORTAL_URL", "WORKBENCH_PUBLIC_URL", "WORKBENCH_INTERNAL_URL", "WORKBENCH_CLIENT_SECRET", "WORKBENCH_GATEWAY_SECRET"} {
		t.Setenv(k, "")
	}
	t.Setenv("WORKBENCH_PORTAL_URL", "http://portal.test")
	t.Setenv("WORKBENCH_PUBLIC_URL", "http://portal.test/apps/assets")
	t.Setenv("WORKBENCH_CLIENT_SECRET", strings.Repeat("s", 32))
	if _, e := FromEnv("assets"); e == nil {
		t.Fatal("path-based mode accepted missing gateway credential")
	}
	t.Setenv("WORKBENCH_GATEWAY_SECRET", strings.Repeat("g", 32))
	if _, e := FromEnv("assets"); e != nil {
		t.Fatal(e)
	}
	t.Setenv("WORKBENCH_PUBLIC_URL", "http://other.test/apps/assets")
	if _, e := FromEnv("assets"); e == nil {
		t.Fatal("cross-origin gateway configuration accepted")
	}
}
