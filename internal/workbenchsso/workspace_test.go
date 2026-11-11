package workbenchsso

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWorkspaceSurvivesSSOWithoutAcceptingRedirectURLs(t *testing.T) {
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/sso/token" {
			fmt.Fprintf(w, `{"token":%q}`, strings.Repeat("t", 64))
			return
		}
		w.WriteHeader(401)
	}))
	defer portal.Close()
	for _, choice := range []string{"finance", "assets", "https://evil.test", "//evil.test"} {
		t.Run(choice, func(t *testing.T) {
			c := &Client{Portal: portal.URL, Internal: portal.URL, Public: "http://app.test", App: "assets", HTTP: portal.Client()}
			handler := c.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("login reached app") }))
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "http://app.test/auth/sso/login", nil)
			q := r.URL.Query()
			q.Set("workspace", choice)
			r.URL.RawQuery = q.Encode()
			handler.ServeHTTP(w, r)
			cookies := w.Result().Cookies()
			state := ""
			for _, cookie := range cookies {
				if strings.HasSuffix(cookie.Name, "_state") {
					state = cookie.Value
				}
			}
			r = httptest.NewRequest("GET", "http://app.test/auth/sso/callback?state="+state+"&code=one", nil)
			for _, cookie := range cookies {
				r.AddCookie(cookie)
			}
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			expected := "/"
			if choice == "finance" || choice == "assets" {
				expected += "?workspace=" + choice
			}
			if w.Code != 303 || w.Header().Get("Location") != expected {
				t.Fatalf("callback: %d %q", w.Code, w.Header().Get("Location"))
			}
			for _, cookie := range w.Result().Cookies() {
				if strings.HasSuffix(cookie.Name, "_workspace") && cookie.MaxAge >= 0 {
					t.Fatal("workspace cookie not cleared")
				}
			}
		})
	}
	c := &Client{Portal: portal.URL, Internal: portal.URL, Public: "http://app.test", App: "assets", HTTP: portal.Client()}
	w := httptest.NewRecorder()
	c.Wrap(http.NotFoundHandler()).ServeHTTP(w, httptest.NewRequest("GET", "http://app.test/?workspace=finance", nil))
	redirect, _ := url.Parse(w.Header().Get("Location"))
	if redirect.Path != "/auth/sso/login" || redirect.Query().Get("workspace") != "finance" || redirect.Query().Get("return") != "/?workspace=finance" {
		t.Fatal("expired session lost workspace", w.Header().Get("Location"))
	}
}
