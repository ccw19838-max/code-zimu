package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleProfile(title string) ProjectProfile {
	return ProjectProfile{Kind: "team", Title: title, Summary: "A project description", SourceNote: "Maintainer-confirmed example"}
}

func TestDemoProfilesIncludeCapabilitiesAndExistingResultImages(t *testing.T) {
	a := githubTestApp(t, true, "")
	w := req(a, "GET", "/api/github/projects", nil, nil, "", false)
	var data githubCatalogResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &data) != nil || len(data.Projects) != 6 {
		t.Fatal("cannot read demo catalog", w.Code)
	}
	kinds := map[string]int{}
	images := 0
	for _, project := range data.Projects {
		p := project.Profile
		if p == nil || len(p.Capabilities) == 0 || len(p.Scenarios) == 0 || len(p.Outputs) == 0 || len(p.Development.EntryPoints) == 0 {
			t.Fatalf("demo project lacks reuse information: %s", project.FullName)
		}
		kinds[p.Kind]++
		for _, screenshot := range p.Screenshots {
			if _, err := fs.Stat(webFiles, "web"+screenshot.Src); err != nil {
				t.Fatalf("missing embedded result image: %s", screenshot.Src)
			}
			images++
		}
	}
	if kinds["team"] != 3 || kinds["reference"] != 3 || images != 3 {
		t.Fatal("demo should distinguish the three team projects and three references", kinds, images)
	}
	for _, path := range []string{"/demo-profiles.json", "/project_profiles.go"} {
		if w := req(a, "GET", path, nil, nil, "", false); w.Code != 404 {
			t.Fatalf("profile source was served publicly at %s", path)
		}
	}
	w = req(a, "GET", "/team-rules.json", nil, nil, "", false)
	var rules struct {
		Status   string `json:"status"`
		Sections []any  `json:"sections"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &rules) != nil || rules.Status != "draft" || len(rules.Sections) == 0 {
		t.Fatal("public team rules are not available to the standalone page", w.Code)
	}
}

func TestLiveProfilesFollowRepositoryVisibility(t *testing.T) {
	profiles := profileFile{Version: 1, Projects: map[string]ProjectProfile{
		"example-team/project-1":              sampleProfile("INTERNAL_PROJECT_ONLY"),
		"EXAMPLE-TEAM/PROJECT-2":              sampleProfile("Shared public project"),
		"example-team/not-returned-by-github": sampleProfile("UNLISTED_PROFILE_ONLY"),
	}}
	filename := filepath.Join(t.TempDir(), "profiles.json")
	b, _ := json.Marshal(profiles)
	if err := os.WriteFile(filename, b, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{PublicURL: "http://portal.test", GitHubOrg: "example-team", ProfilesPath: filename}, "secure-test-password")
	if err != nil {
		t.Fatal(err)
	}
	a.github.client.Transport = githubRoundTripper(func(_ *http.Request) (*http.Response, error) {
		return githubResponse(200, []githubAPIRepository{githubRow(1, true), githubRow(2, false), githubRow(3, false)}), nil
	})
	w := req(a, "GET", "/api/github/projects", nil, nil, "", false)
	var data githubCatalogResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &data) != nil || len(data.Projects) != 2 {
		t.Fatal("anonymous catalog failed", w.Code)
	}
	if strings.Contains(w.Body.String(), "INTERNAL_PROJECT_ONLY") || strings.Contains(w.Body.String(), "UNLISTED_PROFILE_ONLY") {
		t.Fatal("curated metadata bypassed repository visibility")
	}
	if data.Projects[0].Profile == nil || data.Projects[0].Profile.Title != "Shared public project" || data.Projects[1].Profile != nil {
		t.Fatal("case-insensitive profile matching or missing-profile handling failed")
	}
	if !strings.Contains(w.Body.String(), `"profile":null`) {
		t.Fatal("missing descriptions must be explicit rather than invented")
	}
	cookie, _ := loginTest(t, a)
	w = req(a, "GET", "/api/github/projects", nil, cookie, "", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "INTERNAL_PROJECT_ONLY") || strings.Contains(w.Body.String(), "UNLISTED_PROFILE_ONLY") {
		t.Fatal("administrator profile visibility failed")
	}
	if c := newGitHubCatalog(Config{GitHubOrg: "ccw19838-max"}); c.profiles != nil {
		t.Fatal("live mode unexpectedly reused demo descriptions")
	}
}

func TestProfileValidationRejectsUnsafeLinksAndMalformedData(t *testing.T) {
	cases := map[string]func(*ProjectProfile){
		"missing title": func(p *ProjectProfile) { p.Title = "" },
		"unknown kind":  func(p *ProjectProfile) { p.Kind = "published" },
		"external docs": func(p *ProjectProfile) { p.Docs = []ProfileDoc{{Label: "docs", URL: "https://example.net/team/repo"}} },
		"credential url": func(p *ProjectProfile) {
			p.Docs = []ProfileDoc{{Label: "docs", URL: "https://secret@github.com/team/repo"}}
		},
		"javascript docs": func(p *ProjectProfile) { p.Docs = []ProfileDoc{{Label: "docs", URL: "javascript:alert(1)"}} },
		"encoded docs": func(p *ProjectProfile) {
			p.Docs = []ProfileDoc{{Label: "docs", URL: "https://github.com/team/repo/%2e%2e/private"}}
		},
		"remote image": func(p *ProjectProfile) {
			p.Screenshots = []ProfileScreenshot{{Src: "https://example.net/result.png", Alt: "result"}}
		},
		"traversal image": func(p *ProjectProfile) {
			p.Screenshots = []ProfileScreenshot{{Src: "/images/../private.png", Alt: "result"}}
		},
		"executable image": func(p *ProjectProfile) {
			p.Screenshots = []ProfileScreenshot{{Src: "/images/result.svg", Alt: "result"}}
		},
		"absolute entry": func(p *ProjectProfile) {
			p.Development.EntryPoints = []ProfileEntryPoint{{Path: "/etc/passwd", Description: "entry"}}
		},
		"oversized text": func(p *ProjectProfile) { p.Summary = strings.Repeat("x", 4097) },
		"oversized list": func(p *ProjectProfile) { p.Capabilities = make([]string, 25) },
	}
	for name, modify := range cases {
		t.Run(name, func(t *testing.T) {
			p := sampleProfile("Example")
			modify(&p)
			b, _ := json.Marshal(profileFile{Version: 1, Projects: map[string]ProjectProfile{"example-team/project": p}})
			if _, err := readProfiles(strings.NewReader(string(b))); err == nil {
				t.Fatal("invalid profile accepted")
			}
		})
	}
	for _, data := range []string{
		`{"version":1,"projects":{},"unknown":true}`,
		`{"version":1,"projects":{}} {}`,
		strings.Repeat(" ", profileFileLimit+1),
	} {
		if _, err := readProfiles(strings.NewReader(data)); err == nil {
			t.Fatal("invalid or oversized JSON accepted")
		}
	}
}

func TestProfileConfigurationAndExample(t *testing.T) {
	profiles, err := loadProfileFile("examples/profiles.example.json")
	if err != nil || len(profiles) != 1 {
		t.Fatal("documented configuration example is not loadable", err)
	}
	for _, cfg := range []Config{
		{PublicURL: "http://portal.test", GitHubDemo: true, ProfilesPath: "profiles.json"},
		{PublicURL: "http://portal.test", ProfilesPath: "profiles.json"},
	} {
		if err := cfg.validate(); err == nil {
			t.Fatal("profiles must require a live organization")
		}
	}
	t.Chdir(t.TempDir())
	if err := os.Mkdir("web", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("web/profiles.json", []byte(`{"version":1,"projects":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProfileFile("web/profiles.json"); err == nil {
		t.Fatal("configuration was allowed in the public asset directory")
	}
}
