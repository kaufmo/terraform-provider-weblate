package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// categoryTree serves the Project docs with the Categories guides and guides/api, linked the way Weblate links them:
// by absolute API URLs on a host other than the client's.
func categoryTree(t *testing.T) *httptest.Server {
	t.Helper()
	guides := `{"id": 4, "slug": "guides", "name": "Guides", "project": "http://weblate.example/api/projects/docs/",
		"category": null, "url": "http://weblate.example/api/categories/4/"}`
	api := `{"id": 9, "slug": "api", "name": "API", "project": "http://weblate.example/api/projects/docs/",
		"category": "http://weblate.example/api/categories/4/", "url": "http://weblate.example/api/categories/9/"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/categories/4/":
			_, _ = w.Write([]byte(guides))
		case "/api/categories/9/":
			_, _ = w.Write([]byte(api))
		case "/api/projects/docs/categories/":
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`{"next": null, "results": [` + guides + `]}`))
				return
			}
			_, _ = w.Write([]byte(`{"next": "http://weblate.example/api/projects/docs/categories/?page=2",
				"results": [` + api + `]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"detail": "Not found."}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCategorySlugPathWalksUpToTheProject(t *testing.T) {
	c := newTestClient(t, categoryTree(t).URL)
	ctx := context.Background()

	cat, err := c.GetCategory(ctx, 9)
	if err != nil {
		t.Fatalf("GetCategory: %v", err)
	}
	path, err := c.CategorySlugPath(ctx, cat)
	if err != nil {
		t.Fatalf("CategorySlugPath: %v", err)
	}
	if path != "docs/guides/api" {
		t.Errorf("CategorySlugPath = %q, want docs/guides/api", path)
	}
}

func TestFindCategoryMatchesTheSlugPath(t *testing.T) {
	c := newTestClient(t, categoryTree(t).URL)
	ctx := context.Background()

	for path, want := range map[string]int64{"docs/guides/api": 9, "docs/guides": 4, "docs/api": 0,
		"docs/guides/api/more": 0, "other/guides": 0} {
		cat, err := c.FindCategory(ctx, path)
		switch {
		case err != nil:
			t.Errorf("FindCategory(%q): %v", path, err)
		case want == 0 && cat != nil:
			t.Errorf("FindCategory(%q) = %+v, want nil", path, *cat)
		case want != 0 && (cat == nil || cat.ID != want):
			t.Errorf("FindCategory(%q) = %v, want the Category %d", path, cat, want)
		}
	}
}

func TestProjectURLIsAnAbsoluteAPIURL(t *testing.T) {
	c := newTestClient(t, "https://example.com/weblate")
	if got, want := c.ProjectURL("docs"), "https://example.com/weblate/api/projects/docs/"; got != want {
		t.Errorf("ProjectURL = %q, want %q", got, want)
	}
}
