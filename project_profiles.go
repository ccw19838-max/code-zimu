package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const profileFileLimit = 1 << 20

// Profiles are curated descriptions, not GitHub-derived access grants. They
// never create a repository entry or bypass the live repository visibility rule.
type ProjectProfile struct {
	Kind         string              `json:"kind"`
	Title        string              `json:"title"`
	Summary      string              `json:"summary"`
	Scenarios    []string            `json:"scenarios"`
	Capabilities []string            `json:"capabilities"`
	Outputs      []ProfileOutput     `json:"outputs"`
	Development  ProfileDevelopment  `json:"development"`
	Docs         []ProfileDoc        `json:"docs"`
	Screenshots  []ProfileScreenshot `json:"screenshots"`
	Maintainer   string              `json:"maintainer"`
	Maturity     string              `json:"maturity"`
	SourceNote   string              `json:"source_note"`
	LicenseNote  string              `json:"license_note"`
}

type ProfileOutput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ProfileDevelopment struct {
	Requirements    []string            `json:"requirements"`
	QuickStart      []string            `json:"quick_start"`
	EntryPoints     []ProfileEntryPoint `json:"entry_points"`
	ExtensionPoints []string            `json:"extension_points"`
	Validation      []string            `json:"validation"`
	Limitations     []string            `json:"limitations"`
}

type ProfileEntryPoint struct {
	Path        string `json:"path"`
	Description string `json:"description"`
}

type ProfileDoc struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type ProfileScreenshot struct {
	Src     string `json:"src"`
	Alt     string `json:"alt"`
	Caption string `json:"caption"`
}

type profileFile struct {
	Version  int                       `json:"version"`
	Projects map[string]ProjectProfile `json:"projects"`
}

//go:embed demo-profiles.json
var demoProfileJSON []byte

var demoProfiles = func() map[string]*ProjectProfile {
	profiles, err := readProfiles(bytes.NewReader(demoProfileJSON))
	if err != nil {
		panic("invalid embedded demo profiles: " + err.Error())
	}
	return profiles
}()

var profilePathPart = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func loadProfileFile(filename string) (map[string]*ProjectProfile, error) {
	if filename == "" {
		return nil, nil
	}
	if !strings.EqualFold(filepath.Ext(filename), ".json") {
		return nil, errors.New("profile file must use the .json extension and remain outside web assets")
	}
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return nil, errors.New("invalid profile file path")
	}
	webRoot, err := filepath.Abs("web")
	if err != nil {
		return nil, errors.New("cannot resolve static asset directory")
	}
	if relative, err := filepath.Rel(webRoot, absolute); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("profile file must be outside the web directory")
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, errors.New("cannot open profile file")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > profileFileLimit {
		return nil, errors.New("profile file must be a regular file no larger than 1 MiB")
	}
	return readProfiles(f)
}

func readProfiles(reader io.Reader) (map[string]*ProjectProfile, error) {
	b, err := io.ReadAll(io.LimitReader(reader, profileFileLimit+1))
	if err != nil || len(b) > profileFileLimit {
		return nil, errors.New("profile file exceeds 1 MiB or cannot be read")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var file profileFile
	if dec.Decode(&file) != nil || dec.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid profile JSON or unknown fields")
	}
	if file.Version != 1 || file.Projects == nil || len(file.Projects) > 1000 {
		return nil, errors.New("expected version 1 and at most 1000 project profiles")
	}
	profiles := make(map[string]*ProjectProfile, len(file.Projects))
	for fullName, profile := range file.Projects {
		parts := strings.Split(fullName, "/")
		if len(parts) != 2 || !githubOrgRE.MatchString(parts[0]) || !safePathPart(parts[1]) || len(parts[1]) > 100 {
			return nil, errors.New("profile keys must be GitHub owner/repository names")
		}
		key := strings.ToLower(fullName)
		if profiles[key] != nil {
			return nil, errors.New("duplicate profile repository name ignoring case")
		}
		if err := validateProfile(&profile); err != nil {
			return nil, fmt.Errorf("invalid project profile: %w", err)
		}
		profiles[key] = &profile
	}
	return profiles, nil
}

func validateProfile(p *ProjectProfile) error {
	if p.Kind != "team" && p.Kind != "reference" || strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Summary) == "" || strings.TrimSpace(p.SourceNote) == "" {
		return errors.New("kind, title, summary and source_note are required")
	}
	if len(p.Title) > 256 || len(p.Maintainer) > 256 || len(p.Maturity) > 256 {
		return errors.New("title, maintainer and maturity must be at most 256 bytes")
	}
	// All nested text is bounded, and omitted lists become [] consistently.
	if err := normalizeProfileValue(reflect.ValueOf(p).Elem()); err != nil {
		return err
	}
	for _, output := range p.Outputs {
		if strings.TrimSpace(output.Title) == "" || strings.TrimSpace(output.Description) == "" {
			return errors.New("outputs require title and description")
		}
	}
	for _, entry := range p.Development.EntryPoints {
		if entry.Path == "" || entry.Description == "" || strings.HasPrefix(entry.Path, "/") || strings.ContainsAny(entry.Path, "\\:\x00") || path.Clean(entry.Path) != entry.Path || entry.Path == ".." || strings.HasPrefix(entry.Path, "../") {
			return errors.New("entry points require a relative repository path and description")
		}
	}
	for _, doc := range p.Docs {
		if strings.TrimSpace(doc.Label) == "" || !validProfileDocURL(doc.URL) {
			return errors.New("docs require a label and safe HTTPS github.com repository URL")
		}
	}
	for _, screenshot := range p.Screenshots {
		if strings.TrimSpace(screenshot.Alt) == "" || !validProfileScreenshot(screenshot.Src) {
			return errors.New("screenshots require alt text and a local /images/ PNG, JPEG or WebP path")
		}
	}
	return nil
}

func normalizeProfileValue(value reflect.Value) error {
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if err := normalizeProfileValue(value.Field(i)); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if value.Len() > 24 {
			return errors.New("profile lists must contain at most 24 items")
		}
		if value.IsNil() {
			value.Set(reflect.MakeSlice(value.Type(), 0, 0))
		}
		for i := 0; i < value.Len(); i++ {
			if err := normalizeProfileValue(value.Index(i)); err != nil {
				return err
			}
		}
	case reflect.String:
		text := value.String()
		if len(text) > 4096 || !utf8.ValidString(text) {
			return errors.New("profile strings must be valid UTF-8 of at most 4096 bytes")
		}
		for _, char := range text {
			if unicode.IsControl(char) && char != '\n' && char != '\t' {
				return errors.New("profile text contains unsupported control characters")
			}
		}
	}
	return nil
}

func safePathPart(part string) bool {
	return part != "." && part != ".." && profilePathPart.MatchString(part)
}

func validProfileDocURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || strings.Contains(raw, "%") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 2 || !githubOrgRE.MatchString(parts[0]) {
		return false
	}
	for _, part := range parts {
		if !safePathPart(part) {
			return false
		}
	}
	return true
}

func validProfileScreenshot(src string) bool {
	if !strings.HasPrefix(src, "/images/") || path.Clean(src) != src {
		return false
	}
	for _, part := range strings.Split(strings.TrimPrefix(src, "/"), "/") {
		if !safePathPart(part) {
			return false
		}
	}
	switch strings.ToLower(path.Ext(src)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	}
	return false
}
