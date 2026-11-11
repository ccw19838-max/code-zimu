package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const githubCacheTTL = 10 * time.Minute

var githubOrgRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

// GitHubProject intentionally contains metadata only. It never contains a
// credential, a clone URL with credentials, or repository contents.
type GitHubProject struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	FullName      string          `json:"full_name"`
	Description   string          `json:"description"`
	HTMLURL       string          `json:"html_url"`
	Language      string          `json:"language"`
	Topics        []string        `json:"topics"`
	Visibility    string          `json:"visibility"`
	Archived      bool            `json:"archived"`
	DefaultBranch string          `json:"default_branch"`
	UpdatedAt     string          `json:"updated_at"`
	Stars         int             `json:"stars"`
	Profile       *ProjectProfile `json:"profile"`
}

type githubCatalogResponse struct {
	Mode            string          `json:"mode"`
	Organization    string          `json:"organization"`
	OrganizationURL string          `json:"organization_url"`
	UpdatedAt       string          `json:"updated_at"`
	Stale           bool            `json:"stale"`
	Message         string          `json:"message"`
	CanRefresh      bool            `json:"can_refresh"`
	Projects        []GitHubProject `json:"projects"`
}

type githubCatalog struct {
	mu          sync.Mutex
	demo        bool
	org         string
	token       string
	client      *http.Client
	projects    []GitHubProject
	updated     time.Time
	lastAttempt time.Time
	lastError   error
	profiles    map[string]*ProjectProfile
}

func newGitHubCatalog(cfg Config) *githubCatalog {
	c := &githubCatalog{demo: cfg.GitHubDemo, org: cfg.GitHubOrg, client: &http.Client{
		Timeout: 15 * time.Second,
		// Never forward authorization through an upstream redirect.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
	if cfg.GitHubOrg != "" {
		c.token = strings.TrimSpace(os.Getenv("GITHUB_CATALOG_TOKEN"))
	}
	if cfg.GitHubDemo {
		c.profiles = demoProfiles
	}
	return c
}

func (a *App) githubRoutes() {
	a.mux.HandleFunc("GET /api/github/projects", func(w http.ResponseWriter, r *http.Request) {
		_, authenticated := a.session(r)
		a.githubProjects(w, r, authenticated, false)
	})
	a.mux.HandleFunc("POST /api/github/refresh", a.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		a.githubProjects(w, r, true, true)
	}))
}

func (a *App) githubProjects(w http.ResponseWriter, r *http.Request, admin, refresh bool) {
	data, err := a.github.list(r.Context(), admin, refresh)
	if err != nil {
		problem(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOut(w, data)
}

func (c *githubCatalog) list(ctx context.Context, admin, refresh bool) (githubCatalogResponse, error) {
	response := githubCatalogResponse{Mode: "disabled", CanRefresh: false, Projects: []GitHubProject{}, Message: "尚未连接 GitHub 组织。管理员可配置组织目录，或启用演示模式查看效果。"}
	if c.demo {
		response.Mode = "demo"
		response.Organization = "团队项目 · 演示目录"
		response.Message = "演示数据：仓库描述、权限、分支与统计仅用于展示。链接指向现有 GitHub 项目，尚未连接组织。"
		response.CanRefresh = admin
		response.Projects = demoGitHubProjects()
		for i := range response.Projects {
			response.Projects[i].Profile = c.profiles[strings.ToLower(response.Projects[i].FullName)]
		}
		response.UpdatedAt = "2026-11-11T02:00:00Z"
		return response, nil
	}
	if c.org == "" {
		return response, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if refresh || c.updated.IsZero() || time.Since(c.updated) >= githubCacheTTL {
		if !refresh && c.lastError != nil && time.Since(c.lastAttempt) < 30*time.Second {
			return response, c.lastError
		}
		c.lastAttempt = time.Now()
		// The refresh fills a shared cache. One browser disconnecting must not
		// cancel it for everyone; fetch still enforces its own 25-second limit.
		projects, err := c.fetch(context.WithoutCancel(ctx))
		if err != nil {
			// Never keep exposing private metadata after a failed permission check
			// or a partial refresh. A failure is not a successfully empty catalog.
			c.projects = nil
			c.updated = time.Time{}
			c.lastError = err
			return response, err
		}
		c.projects = projects
		c.updated = time.Now().UTC()
		c.lastError = nil
	}
	response.Mode = "live"
	response.Organization = c.org
	response.OrganizationURL = "https://github.com/" + c.org
	response.UpdatedAt = c.updated.Format(time.RFC3339)
	response.CanRefresh = admin
	response.Message = "目录按需更新，缓存有效期为 10 分钟。GitHub 链接由 GitHub 检查访问权限。"
	if admin {
		response.Message += " 私有仓库目录暂仅项目目录管理员可见。"
	} else {
		response.Message += " 当前仅展示公开仓库；尚未绑定个人 GitHub 仓库权限。"
	}
	for _, project := range c.projects {
		if admin || project.Visibility == "public" {
			// Attach editorial metadata only after repository visibility checks.
			project.Profile = c.profiles[strings.ToLower(project.FullName)]
			response.Projects = append(response.Projects, project)
		}
	}
	return response, nil
}

type githubAPIRepository struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	FullName      string   `json:"full_name"`
	Description   string   `json:"description"`
	HTMLURL       string   `json:"html_url"`
	Language      string   `json:"language"`
	Topics        []string `json:"topics"`
	Visibility    string   `json:"visibility"`
	Private       bool     `json:"private"`
	Archived      bool     `json:"archived"`
	DefaultBranch string   `json:"default_branch"`
	UpdatedAt     string   `json:"updated_at"`
	Stars         int      `json:"stargazers_count"`
}

func (c *githubCatalog) fetch(ctx context.Context) ([]GitHubProject, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	projects := []GitHubProject{}
	seen := map[int64]bool{}
	for page := 1; page <= 11; page++ {
		// Construct each page locally. Upstream Link headers cannot redirect
		// this credential to another host, protocol, organization, or path.
		endpoint := fmt.Sprintf("https://api.github.com/orgs/%s/repos?type=all&sort=full_name&direction=asc&per_page=100&page=%d", c.org, page)
		r, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, errors.New("GitHub 请求配置无效")
		}
		r.Header.Set("Accept", "application/vnd.github+json")
		r.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		r.Header.Set("User-Agent", "GitHub-Project-Catalog")
		if c.token != "" {
			r.Header.Set("Authorization", "Bearer "+c.token)
		}
		res, err := c.client.Do(r)
		if err != nil {
			return nil, errors.New("无法连接 GitHub，请稍后重试或检查服务器网络")
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			switch res.StatusCode {
			case http.StatusUnauthorized:
				return nil, errors.New("GitHub 令牌无效或已过期，请管理员更新服务器凭据")
			case http.StatusForbidden, http.StatusTooManyRequests:
				return nil, errors.New("GitHub 拒绝读取或请求额度已用尽，请检查授权并稍后重试")
			case http.StatusNotFound:
				return nil, errors.New("GitHub 组织不存在或当前凭据不可访问")
			default:
				return nil, errors.New("GitHub 暂时无法提供完整目录，请稍后重试")
			}
		}
		const limit = 4 << 20
		b, readErr := io.ReadAll(io.LimitReader(res.Body, limit+1))
		res.Body.Close()
		if readErr != nil || len(b) > limit {
			return nil, errors.New("GitHub 目录响应过大或不完整")
		}
		var rows []githubAPIRepository
		if json.Unmarshal(b, &rows) != nil || rows == nil || len(rows) > 100 {
			return nil, errors.New("GitHub 返回了无法识别的目录数据")
		}
		// An extra empty page confirms that an exact 1000-repository catalog
		// is complete. Any data on this page exceeds the supported limit.
		if page == 11 && len(rows) > 0 {
			return nil, errors.New("组织目录超过 1000 个仓库的读取上限，请管理员缩小授权范围或扩展分页支持")
		}
		for _, row := range rows {
			if !validGitHubRepositoryURL(row.HTMLURL, c.org, row.Name) || row.ID <= 0 || !strings.EqualFold(row.FullName, c.org+"/"+row.Name) {
				return nil, errors.New("GitHub 返回了无效的仓库地址")
			}
			if seen[row.ID] {
				continue
			}
			seen[row.ID] = true
			visibility := "public"
			if row.Private || row.Visibility != "" && row.Visibility != "public" {
				visibility = "private"
			}
			if row.Topics == nil {
				row.Topics = []string{}
			}
			projects = append(projects, GitHubProject{ID: row.ID, Name: row.Name, FullName: row.FullName, Description: row.Description, HTMLURL: row.HTMLURL, Language: row.Language, Topics: row.Topics, Visibility: visibility, Archived: row.Archived, DefaultBranch: row.DefaultBranch, UpdatedAt: row.UpdatedAt, Stars: row.Stars})
		}
		if len(rows) < 100 {
			return projects, nil
		}
	}
	return nil, errors.New("GitHub 目录分页未完成")
}

func validGitHubRepositoryURL(raw, org, name string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || name == "" || strings.ContainsAny(name, "/\\") {
		return false
	}
	parts := strings.Split(u.Path, "/")
	return len(parts) == 3 && parts[0] == "" && strings.EqualFold(parts[1], org) && strings.EqualFold(parts[2], name)
}

func demoGitHubProjects() []GitHubProject {
	return []GitHubProject{
		{ID: 1, Name: "workbench-navigation", FullName: "ccw19838-max/workbench-navigation", Description: "团队应用入口、统一身份与 GitHub 项目目录。", HTMLURL: "https://github.com/ccw19838-max/workbench-navigation", Language: "Go", Topics: []string{"工作台", "统一登录", "团队协作"}, Visibility: "private", DefaultBranch: "dev/cc", UpdatedAt: "2026-11-11T01:30:00Z", Stars: 0},
		{ID: 2, Name: "XRplan", FullName: "ccw19838-max/XRplan", Description: "项目知识库与资料检索，让团队经验随项目沉淀。", HTMLURL: "https://github.com/ccw19838-max/XRplan", Language: "Go", Topics: []string{"知识库", "项目管理"}, Visibility: "private", DefaultBranch: "dev/cc", UpdatedAt: "2026-11-10T08:20:00Z", Stars: 0},
		{ID: 3, Name: "lab-asset-system", FullName: "ccw19838-max/lab-asset-system", Description: "实验室资产、借还、采购和财务管理。", HTMLURL: "https://github.com/ccw19838-max/lab-asset-system", Language: "Go", Topics: []string{"实验室", "资产管理", "财务"}, Visibility: "private", DefaultBranch: "dev/cc", UpdatedAt: "2026-11-09T04:15:00Z", Stars: 0},
		{ID: 4, Name: "go", FullName: "golang/go", Description: "Go 语言项目，作为开源工具类仓库的展示样例。", HTMLURL: "https://github.com/golang/go", Language: "Go", Topics: []string{"开源样例", "工具链"}, Visibility: "public", DefaultBranch: "master", UpdatedAt: "2026-11-08T02:00:00Z", Stars: 0},
		{ID: 5, Name: "vscode", FullName: "microsoft/vscode", Description: "代码编辑器项目，作为前端与开发工具的展示样例。", HTMLURL: "https://github.com/microsoft/vscode", Language: "TypeScript", Topics: []string{"开源样例", "开发工具"}, Visibility: "public", DefaultBranch: "main", UpdatedAt: "2026-11-07T02:00:00Z", Stars: 0},
		{ID: 6, Name: "flask", FullName: "pallets/flask", Description: "Python Web 框架项目，作为参考资料类仓库的展示样例。", HTMLURL: "https://github.com/pallets/flask", Language: "Python", Topics: []string{"开源样例", "Web"}, Visibility: "public", DefaultBranch: "main", UpdatedAt: "2026-11-06T02:00:00Z", Stars: 0},
	}
}
