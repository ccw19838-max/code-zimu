package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type githubRoundTripper func(*http.Request) (*http.Response, error)

func (fn githubRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func githubResponse(status int, value any) *http.Response {
	b, _ := json.Marshal(value)
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(b)))}
}

func githubTestApp(t *testing.T, demo bool, org string) *App {
	t.Helper()
	t.Setenv("GITHUB_CATALOG_TOKEN", "test-private-token-never-display")
	a, err := New(Config{PublicURL: "http://catalog.test", GitHubDemo: demo, GitHubOrg: org}, "secure-test-password")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func githubTestMember(a *App) *http.Cookie {
	// A navigation application's cookie never authenticates this application.
	return &http.Cookie{Name: "workbench_session", Value: "member-session"}
}

func githubRow(id int, private bool) githubAPIRepository {
	name := fmt.Sprintf("project-%d", id)
	return githubAPIRepository{ID: int64(id), Name: name, FullName: "example-team/" + name, HTMLURL: "https://github.com/example-team/" + name, Private: private, Description: "project description", Language: "Go", DefaultBranch: "main"}
}

func TestGitHubDemoRoutesAndAuthorization(t *testing.T) {
	a := githubTestApp(t, true, "")
	if w := req(a, "GET", "/api/github/projects", nil, nil, "", false); w.Code != 200 {
		t.Fatalf("anonymous request: %d", w.Code)
	}
	cookie, csrf := loginTest(t, a)
	w := req(a, "GET", "/api/github/projects", nil, cookie, "", false)
	var data githubCatalogResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &data) != nil || data.Mode != "demo" || len(data.Projects) != 6 || !data.CanRefresh || data.OrganizationURL != "" {
		t.Fatalf("unexpected demo response: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "test-private-token") || !strings.Contains(data.Message, "演示") {
		t.Fatal("demo leaks a token or omits the sample-data label")
	}
	if w := req(a, "POST", "/api/github/refresh", map[string]any{}, cookie, "", false); w.Code != 403 {
		t.Fatalf("refresh accepted without CSRF: %d", w.Code)
	}
	if w := req(a, "POST", "/api/github/refresh", map[string]any{}, cookie, csrf, false); w.Code != 200 {
		t.Fatalf("admin refresh failed: %d", w.Code)
	}
	member := githubTestMember(a)
	if w := req(a, "POST", "/api/github/refresh", map[string]any{}, member, "member-csrf", false); w.Code != 401 {
		t.Fatalf("member refreshed the organization catalog: %d", w.Code)
	}
	w = req(a, "GET", "/api/github/projects", nil, member, "", false)
	if json.Unmarshal(w.Body.Bytes(), &data) != nil || data.CanRefresh || len(data.Projects) != 6 {
		t.Fatal("members should see the explicitly illustrative demo, not admin refresh")
	}
}

func TestGitHubDisabledAndConfigValidation(t *testing.T) {
	a := githubTestApp(t, false, "")
	data, err := a.github.list(context.Background(), true, false)
	if err != nil || data.Mode != "disabled" || len(data.Projects) != 0 || data.CanRefresh {
		t.Fatal("disabled configuration unexpectedly exposes data or refresh", data, err)
	}
	for _, cfg := range []Config{{GitHubOrg: "https://github.com/example-team"}, {GitHubOrg: "../admin"}, {GitHubOrg: "team", GitHubDemo: true}} {
		cfg.PublicURL = "http://catalog.test"
		if err := cfg.validate(); err == nil {
			t.Fatal("invalid configuration accepted", cfg)
		}
	}
}

func TestGitHubLivePaginationCacheAndMemberPrivacy(t *testing.T) {
	a := githubTestApp(t, false, "example-team")
	var requests atomic.Int32
	a.github.client.Transport = githubRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.URL.Host != "api.github.com" || r.URL.Scheme != "https" || r.URL.Path != "/orgs/example-team/repos" || r.Header.Get("Authorization") != "Bearer test-private-token-never-display" {
			t.Errorf("unexpected GitHub request target or authentication")
		}
		if r.URL.Query().Get("sort") != "full_name" || r.URL.Query().Get("direction") != "asc" {
			t.Error("pagination should use stable repository-name ordering")
		}
		if r.URL.Query().Get("page") == "1" {
			rows := []githubAPIRepository{}
			for i := 1; i <= 100; i++ {
				rows = append(rows, githubRow(i, i%2 == 0))
			}
			res := githubResponse(200, rows)
			res.Header.Set("Link", `<https://attacker.invalid/?token=steal>; rel="next"`)
			return res, nil
		}
		if r.URL.Query().Get("page") != "2" {
			t.Error("unexpected pagination request")
		}
		return githubResponse(200, []githubAPIRepository{githubRow(101, false)}), nil
	})
	cookie, _ := loginTest(t, a)
	w := req(a, "GET", "/api/github/projects", nil, cookie, "", false)
	var admin githubCatalogResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &admin) != nil || len(admin.Projects) != 101 || admin.UpdatedAt == "" {
		t.Fatalf("admin catalog failed: %d %s", w.Code, w.Body)
	}
	member := githubTestMember(a)
	w = req(a, "GET", "/api/github/projects", nil, member, "", false)
	var visible githubCatalogResponse
	if json.Unmarshal(w.Body.Bytes(), &visible) != nil || len(visible.Projects) != 51 || visible.CanRefresh || visible.Mode != "live" {
		t.Fatalf("member filtering failed: %s", w.Body)
	}
	for _, project := range visible.Projects {
		if project.Visibility != "public" {
			t.Fatal("private project leaked to member")
		}
	}
	if requests.Load() != 2 || strings.Contains(w.Body.String(), "test-private-token") {
		t.Fatal("cache was not reused or a credential leaked")
	}
	a.github.updated = time.Now().Add(-githubCacheTTL)
	if _, err := a.github.list(context.Background(), true, false); err != nil || requests.Load() != 4 {
		t.Fatal("expired catalog did not refresh", err)
	}
}

func TestGitHubFailedRefreshClearsPrivateCacheAndSanitizesErrors(t *testing.T) {
	a := githubTestApp(t, false, "example-team")
	var requests atomic.Int32
	a.github.client.Transport = githubRoundTripper(func(r *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return githubResponse(200, []githubAPIRepository{githubRow(1, true)}), nil
		}
		return githubResponse(401, map[string]string{"message": "test-private-token-never-display upstream diagnostic"}), nil
	})
	cookie, csrf := loginTest(t, a)
	if w := req(a, "GET", "/api/github/projects", nil, cookie, "", false); w.Code != 200 {
		t.Fatal(w.Code)
	}
	w := req(a, "POST", "/api/github/refresh", map[string]any{}, cookie, csrf, false)
	if w.Code != 502 || strings.Contains(w.Body.String(), "test-private-token") || strings.Contains(w.Body.String(), "upstream diagnostic") || len(a.github.projects) != 0 || !a.github.updated.IsZero() {
		t.Fatalf("failed refresh retained private data or exposed raw upstream error: %d %s", w.Code, w.Body)
	}
	w = req(a, "GET", "/api/github/projects", nil, cookie, "", false)
	if w.Code != 502 || requests.Load() != 2 {
		t.Fatal("failed request was not briefly backed off")
	}
}

func TestGitHubRejectsUnsafeOrPartialResponses(t *testing.T) {
	for _, variant := range []string{"external", "userinfo", "query", "fragment", "wrong-owner", "encoded-path", "extra-component", "truncated", "oversize", "redirect", "limit"} {
		t.Run(variant, func(t *testing.T) {
			c := newGitHubCatalog(Config{GitHubOrg: "example-team"})
			c.client.Transport = githubRoundTripper(func(r *http.Request) (*http.Response, error) {
				row := githubRow(1, false)
				switch variant {
				case "external":
					row.HTMLURL = "https://example.net/example-team/project-1"
				case "userinfo":
					row.HTMLURL = "https://attacker@github.com/example-team/project-1"
				case "query":
					row.HTMLURL += "?credential=secret"
				case "fragment":
					row.HTMLURL += "#secret"
				case "wrong-owner":
					row.FullName = "other-org/project-1"
				case "encoded-path":
					row.HTMLURL = "https://github.com/example-team/%70roject-1"
				case "extra-component":
					row.HTMLURL += "/issues"
				case "truncated":
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[{"id":1`))}, nil
				case "oversize":
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat(" ", (4<<20)+1)))}, nil
				case "redirect":
					return &http.Response{StatusCode: 301, Header: http.Header{"Location": []string{"https://attacker.invalid/"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
				case "limit":
					rows := make([]githubAPIRepository, 100)
					for i := range rows {
						rows[i] = githubRow(i+1, false)
					}
					return githubResponse(200, rows), nil
				}
				return githubResponse(200, []githubAPIRepository{row}), nil
			})
			if rows, err := c.fetch(context.Background()); err == nil || rows != nil {
				t.Fatal("unsafe, redirected or partial response accepted")
			}
		})
	}
}

func TestGitHubAcceptsCanonicalOwnerCasing(t *testing.T) {
	c := newGitHubCatalog(Config{GitHubOrg: "EXAMPLE-TEAM"})
	c.client.Transport = githubRoundTripper(func(r *http.Request) (*http.Response, error) {
		row := githubRow(1, false)
		row.Name = "Project-1"
		return githubResponse(200, []githubAPIRepository{row}), nil
	})
	rows, err := c.fetch(context.Background())
	if err != nil || len(rows) != 1 || rows[0].HTMLURL != "https://github.com/example-team/project-1" {
		t.Fatal("canonical GitHub casing was rejected", rows, err)
	}
}

func TestGitHubConcurrentReadersShareFetch(t *testing.T) {
	c := newGitHubCatalog(Config{GitHubOrg: "example-team"})
	var requests atomic.Int32
	c.client.Transport = githubRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		return githubResponse(200, []githubAPIRepository{githubRow(1, false)}), nil
	})
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			data, err := c.list(context.Background(), false, false)
			if err != nil || len(data.Projects) != 1 {
				t.Error("concurrent catalog read failed", err)
			}
		}()
	}
	group.Wait()
	if requests.Load() != 1 {
		t.Fatal("concurrent reads duplicated the GitHub request")
	}
}

func TestGitHubCancelledCallerDoesNotPoisonSharedCache(t *testing.T) {
	c := newGitHubCatalog(Config{GitHubOrg: "example-team"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var requests int
	c.client.Transport = githubRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		cancel() // Simulate the browser closing while GitHub is being fetched.
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		if deadline, ok := r.Context().Deadline(); !ok || time.Until(deadline) > 25*time.Second {
			t.Error("shared refresh must retain a bounded timeout")
		}
		return githubResponse(200, []githubAPIRepository{githubRow(1, false)}), nil
	})
	data, err := c.list(ctx, false, false)
	if ctx.Err() != context.Canceled || err != nil || len(data.Projects) != 1 || c.lastError != nil {
		t.Fatal("cancelled caller poisoned shared refresh", data, err)
	}
	data, err = c.list(context.Background(), false, false)
	if err != nil || len(data.Projects) != 1 || requests != 1 {
		t.Fatal("next caller could not use completed shared cache", data, err)
	}
}

func TestGitHubCatalogSizeBoundary(t *testing.T) {
	for _, total := range []int{1000, 1001} {
		t.Run(strconv.Itoa(total), func(t *testing.T) {
			c := newGitHubCatalog(Config{GitHubOrg: "example-team"})
			var requests int
			c.client.Transport = githubRoundTripper(func(r *http.Request) (*http.Response, error) {
				requests++
				page, err := strconv.Atoi(r.URL.Query().Get("page"))
				if err != nil || page < 1 || page > 11 {
					t.Fatal("unbounded or invalid pagination", r.URL)
				}
				rows := []githubAPIRepository{}
				for id := (page-1)*100 + 1; id <= page*100 && id <= total; id++ {
					rows = append(rows, githubRow(id, false))
				}
				return githubResponse(200, rows), nil
			})
			rows, err := c.fetch(context.Background())
			if requests != 11 {
				t.Fatal("catalog boundary should be confirmed on page 11", requests)
			}
			if total == 1000 {
				if err != nil || len(rows) != total {
					t.Fatal("exactly 1000 repositories should be accepted", len(rows), err)
				}
			} else if err == nil || rows != nil || !strings.Contains(err.Error(), "超过 1000") {
				t.Fatal("oversized catalog should fail without partial metadata", len(rows), err)
			}
		})
	}
}
