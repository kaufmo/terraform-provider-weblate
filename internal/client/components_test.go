package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// storedComponent is the Component docs/guides/api/reference in the Category guides/api of categoryTree.
const storedComponent = `{"id": 12, "slug": "reference", "name": "Reference",
	"project": {"id": 7, "slug": "docs", "url": "http://weblate.example/api/projects/docs/"},
	"category": "http://weblate.example/api/categories/9/", "source_language": {"code": "en", "name": "English"},
	"branch": "main", "file_format_params": {"po_line_wrap": 77},
	"url": "http://weblate.example/api/components/docs/guides%252Fapi%252Freference/",
	"task_url": "http://weblate.example/api/tasks/abc/"}`

func TestComponentRefEncodesTheCategoryPathTwice(t *testing.T) {
	var requestURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(storedComponent))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)

	if _, err := c.GetComponent(context.Background(), ComponentRef("docs/guides/api/reference")); err != nil {
		t.Fatalf("GetComponent: %v", err)
	}
	if want := "/api/components/docs/guides%252Fapi%252Freference/"; requestURI != want {
		t.Errorf("request URI = %s, want %s", requestURI, want)
	}

	// Weblate's own URLs carry the same encoding, and the client must send them unchanged.
	if _, err := c.GetComponent(context.Background(), "http://weblate.example/api/components/docs/a%252Fb/"); err != nil {
		t.Fatalf("GetComponent: %v", err)
	}
	if want := "/api/components/docs/a%252Fb/"; requestURI != want {
		t.Errorf("request URI = %s, want %s", requestURI, want)
	}
}

func TestCreateComponentPostsToTheProject(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(storedComponent))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)

	comp, err := c.CreateComponent(context.Background(), "docs", map[string]any{"name": "Reference"})
	if err != nil {
		t.Fatalf("CreateComponent: %v", err)
	}
	if method != http.MethodPost || path != "/api/projects/docs/components/" {
		t.Errorf("request = %s %s, want POST /api/projects/docs/components/", method, path)
	}
	if comp.ID != 12 || comp.Project.Slug != "docs" || comp.SourceLanguage.Code != "en" ||
		comp.TaskURL != "http://weblate.example/api/tasks/abc/" {
		t.Errorf("component = %+v", comp)
	}
	if got := string(comp.Fields["branch"]); got != `"main"` {
		t.Errorf("Fields[branch] = %s, want the raw JSON of every field", got)
	}
}

func TestComponentSlugPathIncludesTheCategories(t *testing.T) {
	c := newTestClient(t, categoryTree(t).URL)
	var comp Component
	if err := comp.UnmarshalJSON([]byte(storedComponent)); err != nil {
		t.Fatal(err)
	}

	got, err := c.ComponentSlugPath(context.Background(), &comp)
	if err != nil || got != "docs/guides/api/reference" {
		t.Errorf("ComponentSlugPath = %q, %v, want docs/guides/api/reference", got, err)
	}

	comp.Category = nil
	if got, _ := c.ComponentSlugPath(context.Background(), &comp); got != "docs/reference" {
		t.Errorf("ComponentSlugPath at the Project root = %q, want docs/reference", got)
	}
}

func TestFindComponentByIDSearchesTheProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/projects/docs/components/" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"detail": "Not found."}`))
			return
		}
		_, _ = w.Write([]byte(`{"next": null, "results": [{"id": 3, "slug": "glossary"}, ` + storedComponent + `]}`))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)
	ctx := context.Background()

	if comp, err := c.FindComponentByID(ctx, "docs", 12); err != nil || comp == nil || comp.Slug != "reference" {
		t.Errorf("FindComponentByID(docs, 12) = %v, %v", comp, err)
	}
	if comp, err := c.FindComponentByID(ctx, "docs", 99); err != nil || comp != nil {
		t.Errorf("FindComponentByID(docs, 99) = %v, %v, want nil", comp, err)
	}
}

func TestFindComponentByIDSearchesAllComponentsWhenTheProjectIsGone(t *testing.T) {
	var lists []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/components/" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"detail": "Not found."}`))
			return
		}
		lists = append(lists, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"next": null, "results": [{"id": 3, "slug": "glossary"}, ` + storedComponent + `]}`))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)
	ctx := context.Background()

	// A Project renamed outside OpenTofu has no Components under its old slug.
	if comp, err := c.FindComponentByID(ctx, "old", 12); err != nil || comp == nil || comp.Slug != "reference" {
		t.Errorf("FindComponentByID(old, 12) = %v, %v", comp, err)
	}
	if comp, err := c.FindComponentByID(ctx, "gone", 99); err != nil || comp != nil {
		t.Errorf("FindComponentByID(gone, 99) = %v, %v, want nil", comp, err)
	}
	if len(lists) != 2 || lists[0] != "page_size=1000" {
		t.Errorf("queries of the Component list = %q, want page_size=1000 each", lists)
	}
}

// taskServer answers GET /api/tasks/abc/ with the given bodies in turn, repeating the last one.
func taskServer(t *testing.T, bodies ...string) *httptest.Server {
	t.Helper()
	gets := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(bodies[min(gets, len(bodies)-1)]))
		gets++
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWaitForTaskPollsUntilCompleted(t *testing.T) {
	c := newTestClient(t, taskServer(t, `{"completed": false, "progress": 10}`,
		`{"completed": true, "progress": 100, "result": {"component": 12}}`).URL)
	sleeps := noSleep(c)

	if err := c.WaitForTask(context.Background(), "tasks/abc/"); err != nil {
		t.Fatalf("WaitForTask: %v", err)
	}
	if *sleeps != 1 {
		t.Errorf("sleeps = %d, want 1", *sleeps)
	}
}

func TestWaitForTaskReportsAFailedTask(t *testing.T) {
	c := newTestClient(t, taskServer(t,
		`{"completed": true, "progress": 100, "result": "Failed to parse po/de.po", "log": "Parsing po/de.po"}`).URL)
	noSleep(c)

	err := c.WaitForTask(context.Background(), "tasks/abc/")
	for _, want := range []string{"Failed to parse po/de.po", "Parsing po/de.po"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("WaitForTask error = %v, want one containing %q", err, want)
		}
	}
}

func TestWaitForTaskReportsProgressOnTimeout(t *testing.T) {
	c := newTestClient(t, taskServer(t, `{"completed": false, "progress": 40}`).URL)
	ctx, cancel := context.WithCancel(context.Background())
	c.sleep = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}

	err := c.WaitForTask(ctx, "tasks/abc/")
	if err == nil || !strings.Contains(err.Error(), "40%") {
		t.Errorf("WaitForTask error = %v, want one with the progress 40%%", err)
	}
}
