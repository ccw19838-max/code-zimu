package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func req(a *App, method, path string, body any, cookie *http.Cookie, csrf string, _ bool) *httptest.ResponseRecorder {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, a.cfg.PublicURL+path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", a.cfg.PublicURL)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}

func loginTest(t *testing.T, a *App) (*http.Cookie, string) {
	t.Helper()
	w := req(a, "POST", "/api/login", map[string]string{"username": "admin", "password": "secure-test-password"}, nil, "", false)
	var result sessionResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.Authenticated || result.CSRF == "" {
		t.Fatalf("login failed: %d %s", w.Code, w.Body)
	}
	if len(w.Result().Cookies()) != 1 {
		t.Fatal("expected one session cookie")
	}
	return w.Result().Cookies()[0], result.CSRF
}

func TestSessionLifecycleAndCookieIsolation(t *testing.T) {
	a := githubTestApp(t, true, "")
	a.cfg.NavigationURL = "http://navigation.test:18090"
	w := req(a, "GET", "/api/session", nil, nil, "", false)
	var state sessionResponse
	if json.Unmarshal(w.Body.Bytes(), &state) != nil || state.Authenticated || state.CSRF != "" || state.Mode != "demo" || state.NavigationURL != a.cfg.NavigationURL {
		t.Fatal("invalid anonymous session response", w.Body)
	}
	cookie, csrf := loginTest(t, a)
	if cookie.Name != "github_catalog_session" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge != 8*60*60 {
		t.Fatal("incorrect standalone cookie settings", cookie)
	}
	for _, supplied := range []*http.Cookie{cookie, {Name: "workbench_session", Value: cookie.Value}} {
		w := req(a, "GET", "/api/session", nil, supplied, "", false)
		if json.Unmarshal(w.Body.Bytes(), &state) != nil || state.Authenticated != (supplied.Name == sessionCookie) {
			t.Fatal("cookie namespace isolation failed", w.Body)
		}
	}
	if w := req(a, "POST", "/api/logout", nil, cookie, "", false); w.Code != 403 {
		t.Fatal("logout accepted without CSRF", w.Code)
	}
	w = req(a, "POST", "/api/logout", nil, cookie, csrf, false)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil || state.Authenticated || state.CSRF != "" || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout failed to clear session", w.Code, w.Body)
	}
	if w := req(a, "POST", "/api/github/refresh", nil, cookie, csrf, false); w.Code != 401 {
		t.Fatal("logged-out session could still refresh", w.Code)
	}
	cookie, _ = loginTest(t, a)
	a.mu.Lock()
	session := a.sessions[cookie.Value]
	session.Expires = time.Now().Add(-time.Second)
	a.sessions[cookie.Value] = session
	a.mu.Unlock()
	w = req(a, "GET", "/api/session", nil, cookie, "", false)
	if json.Unmarshal(w.Body.Bytes(), &state) != nil || state.Authenticated || len(a.sessions) != 0 {
		t.Fatal("expired session was accepted")
	}
}

func TestHTTPSCookieAndLoginRotation(t *testing.T) {
	a, err := New(Config{PublicURL: "https://catalog.test", GitHubDemo: true}, "secure-test-password")
	if err != nil {
		t.Fatal(err)
	}
	old, csrf := loginTest(t, a)
	if !old.Secure {
		t.Fatal("HTTPS must set Secure cookie")
	}
	w := req(a, "POST", "/api/login", map[string]string{"username": "admin", "password": "secure-test-password"}, old, "", false)
	if w.Code != 200 || w.Result().Cookies()[0].Value == old.Value {
		t.Fatal("login did not rotate the session", w.Body)
	}
	if w := req(a, "POST", "/api/github/refresh", nil, old, csrf, false); w.Code != 401 {
		t.Fatal("rotated session remained valid", w.Code)
	}
}

func TestHostOriginAndStaticBoundaries(t *testing.T) {
	a := githubTestApp(t, true, "")
	for _, test := range []struct {
		method, path, host, origin string
		status                     int
	}{
		{"GET", "/api/github/projects", "attacker.test", "", 421},
		{"GET", "/api/session", "catalog.test", "http://catalog.test:18090", 403},
		{"POST", "/api/login", "catalog.test", "http://other.test", 403},
		{"POST", "/api/login", "catalog.test", "", 403},
		{"GET", "/api/missing", "catalog.test", "", 404},
		{"GET", "/missing.html", "catalog.test", "", 404},
		{"POST", "/", "catalog.test", "http://catalog.test", 405},
		{"GET", "/", "catalog.test", "", 200},
		{"GET", "/healthz", "catalog.test", "", 200},
	} {
		r := httptest.NewRequest(test.method, "http://"+test.host+test.path, nil)
		r.Header.Set("Origin", test.origin)
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Errorf("%s %s host=%s origin=%s: got %d want %d", test.method, test.path, test.host, test.origin, w.Code, test.status)
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("security headers missing or cross-origin access enabled")
		}
	}
}

func TestLoginValidationAndRateLimit(t *testing.T) {
	a := githubTestApp(t, true, "")
	for _, payload := range []string{`{"username":"admin","password":"x","unknown":true}`, `{"username":"admin"} {}`, strings.Repeat("x", 4097)} {
		r := httptest.NewRequest("POST", a.cfg.PublicURL+"/api/login", strings.NewReader(payload))
		r.Header.Set("Origin", a.cfg.PublicURL)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("invalid or oversized JSON accepted", w.Code)
		}
	}
	for i := 0; i < 7; i++ {
		w := req(a, "POST", "/api/login", map[string]string{"username": "admin", "password": "wrong"}, nil, "", false)
		if w.Code != 401 || strings.Contains(w.Body.String(), "wrong") {
			t.Fatal("invalid password handling failed", w.Code, w.Body)
		}
	}
	w := req(a, "POST", "/api/login", map[string]string{"username": "admin", "password": "secure-test-password"}, nil, "", false)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("login rate limit missing", w.Code)
	}
}

func TestStandaloneConfigAndAnonymousDemo(t *testing.T) {
	for _, test := range []struct {
		config   Config
		password string
	}{
		{Config{PublicURL: "http://catalog.test", GitHubOrg: "example-team"}, ""},
		{Config{PublicURL: "http://catalog.test", GitHubDemo: true}, "short"},
		{Config{PublicURL: "http://catalog.test/path", GitHubDemo: true}, ""},
		{Config{PublicURL: "http://catalog.test?", GitHubDemo: true}, ""},
		{Config{PublicURL: "http://catalog.test#", GitHubDemo: true}, ""},
		{Config{PublicURL: "http://:80", GitHubDemo: true}, ""},
		{Config{PublicURL: "http://catalog.test", NavigationURL: "javascript:alert(1)"}, ""},
	} {
		if _, err := New(test.config, test.password); err == nil {
			t.Fatal("invalid standalone configuration accepted")
		}
	}
	a, err := New(Config{PublicURL: "http://catalog.test", GitHubDemo: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if w := req(a, "GET", "/api/github/projects", nil, nil, "", false); w.Code != 200 {
		t.Fatal("demo must be browsable without login")
	}
	if w := req(a, "POST", "/api/login", map[string]string{"username": "admin", "password": ""}, nil, "", false); w.Code != 403 {
		t.Fatal("unconfigured demo admin unexpectedly authenticated")
	}
}
