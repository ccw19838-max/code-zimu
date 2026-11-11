package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github-catalog/internal/workbenchsso"
)

func TestWorkbenchRolesPrivateMetadataAndRevocation(t *testing.T) {
	var state atomic.Int32
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app, secret, _ := r.BasicAuth()
		if app != "code-zimu" || secret != strings.Repeat("s", 32) {
			t.Error("wrong SSO client authentication")
		}
		if state.Load() == 2 {
			w.WriteHeader(401)
			return
		}
		if state.Load() == 3 {
			w.WriteHeader(503)
			return
		}
		role := "user"
		if state.Load() == 1 {
			role = "admin"
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"subject":"team_member","account":"existing_id","name":"成员","role":%q}`, role)
	}))
	defer portal.Close()
	c := &workbenchsso.Client{Portal: portal.URL, Internal: portal.URL, Public: "http://catalog.test", App: "code-zimu", Secret: strings.Repeat("s", 32), HTTP: portal.Client()}
	a, err := New(Config{PublicURL: c.Public, GitHubOrg: "example-team", SSO: c}, "ignored-old-password")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.passwordHash) != 0 {
		t.Fatal("SSO retained a local password bypass")
	}
	a.github.client = &http.Client{Transport: githubRoundTripper(func(r *http.Request) (*http.Response, error) {
		return githubResponse(200, []githubAPIRepository{githubRow(1, false), githubRow(2, true)}), nil
	})}
	cookie := &http.Cookie{Name: "wb_code-zimu_session", Value: strings.Repeat("t", 64)}
	w := req(a, "GET", "/api/session", nil, cookie, "", false)
	var session sessionResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &session) != nil || !session.Authenticated || session.Role != "user" || session.AuthMode != "workbench" || session.NavigationURL != portal.URL || len(session.CSRF) != 64 {
		t.Fatal("member session metadata", w.Code, w.Body)
	}
	w = req(a, "GET", "/api/github/projects", nil, cookie, "", false)
	var catalog githubCatalogResponse
	if json.Unmarshal(w.Body.Bytes(), &catalog) != nil || len(catalog.Projects) != 1 || catalog.Projects[0].Visibility != "public" || catalog.CanRefresh {
		t.Fatal("member received private metadata or refresh permission", w.Body)
	}
	if w := req(a, "POST", "/api/github/refresh", map[string]any{}, cookie, session.CSRF, false); w.Code != 403 {
		t.Fatal("member refreshed catalog", w.Code)
	}
	if w := req(a, "POST", "/api/login", map[string]string{"username": "admin", "password": "ignored-old-password"}, cookie, "", false); w.Code != 403 {
		t.Fatal("local login bypassed SSO", w.Code)
	}
	state.Store(1)
	w = req(a, "GET", "/api/github/projects", nil, cookie, "", false)
	if json.Unmarshal(w.Body.Bytes(), &catalog) != nil || len(catalog.Projects) != 2 || !catalog.CanRefresh {
		t.Fatal("catalog admin did not receive explicit catalog rights", w.Body)
	}
	if w := req(a, "POST", "/api/github/refresh", map[string]any{}, cookie, "", false); w.Code != 403 {
		t.Fatal("SSO admin bypassed CSRF", w.Code)
	}
	if w := req(a, "POST", "/api/github/refresh", map[string]any{}, cookie, session.CSRF, false); w.Code != 200 {
		t.Fatal("SSO admin refresh failed", w.Code, w.Body)
	}
	state.Store(2)
	for _, path := range []string{"/api/session", "/api/github/projects", "/team-rules.json"} {
		w = req(a, "GET", path, nil, cookie, "", false)
		if path == "/team-rules.json" {
			if w.Code != 303 {
				t.Fatal("revoked member read protected static content", w.Code)
			}
		} else if w.Code != 401 {
			t.Fatal("revoked member retained API access", w.Code)
		}
	}
	if w := req(a, "GET", "/api/session", nil, &http.Cookie{Name: sessionCookie, Value: "old-local-session"}, "", false); w.Code != 401 {
		t.Fatal("old standalone cookie bypassed SSO", w.Code)
	}
	state.Store(3)
	if w := req(a, "GET", "/api/github/projects", nil, cookie, "", false); w.Code != 503 {
		t.Fatal("offline SSO did not fail closed", w.Code)
	}
	if w := req(a, "GET", "/healthz", nil, nil, "", false); w.Code != 200 {
		t.Fatal("liveness depends on central login", w.Code)
	}
}
