package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type project struct {
	Slug string `json:"slug"`
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := New(baseURL, "wlu_secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestDoSendsTokenAndDecodesJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Token wlu_secret" {
			t.Errorf("Authorization = %q", got)
		}
		if r.URL.Path != "/weblate/api/projects/docs/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"slug":"docs"}`))
	}))
	defer srv.Close()

	for _, base := range []string{srv.URL + "/weblate", srv.URL + "/weblate/"} {
		var p project
		if err := newTestClient(t, base).Do(context.Background(), http.MethodGet, "projects/docs/", nil, &p); err != nil {
			t.Fatalf("Do(%s): %v", base, err)
		}
		if p.Slug != "docs" {
			t.Errorf("slug = %q", p.Slug)
		}
	}
}

func TestNewRejectsInvalidURLs(t *testing.T) {
	for _, u := range []string{
		"ftp://weblate.example.com",
		"weblate.example.com",
		"https://",
		"https://weblate.example.com/api",
		"https://weblate.example.com/api/",
		"https://example.com/weblate/api",
	} {
		if _, err := New(u, "token"); err == nil {
			t.Errorf("New(%q) accepted the URL", u)
		}
	}
	for _, u := range []string{"http://weblate.example.com", "https://weblate.example.com/", "https://example.com/weblate"} {
		if _, err := New(u, "token"); err != nil {
			t.Errorf("New(%q): %v", u, err)
		}
	}
}
