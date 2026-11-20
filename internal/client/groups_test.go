package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetGroupDecodesTheGroup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/groups/12/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 12, "name": "Reviewers", "defining_project": "https://weblate.example/api/projects/docs/",
			"project_selection": 3, "language_selection": 1, "enforced_2fa": true, "roles": []}`))
	}))
	defer srv.Close()

	g, err := newTestClient(t, srv.URL).GetGroup(context.Background(), 12)
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if g.ID != 12 || g.Name != "Reviewers" || g.DefiningProject == nil ||
		*g.DefiningProject != "https://weblate.example/api/projects/docs/" || g.ProjectSelection != 3 ||
		g.LanguageSelection != 1 || !g.Enforced2FA {
		t.Errorf("GetGroup = %+v", *g)
	}
}

func TestCreateGroupPostsTheFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/api/groups/" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if string(body) != `{"name":"Reviewers","project_selection":1}` {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 12, "name": "Reviewers", "defining_project": null, "project_selection": 1}`))
	}))
	defer srv.Close()

	g, err := newTestClient(t, srv.URL).CreateGroup(context.Background(),
		map[string]any{"name": "Reviewers", "project_selection": 1})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if g.ID != 12 || g.DefiningProject != nil || g.ProjectSelection != 1 {
		t.Errorf("CreateGroup = %+v", *g)
	}
}

func TestUpdateGroupPatchesTheGroup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPatch || r.URL.Path != "/api/groups/12/" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if string(body) != `{"name":"Translators"}` {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 12, "name": "Translators"}`))
	}))
	defer srv.Close()

	g, err := newTestClient(t, srv.URL).UpdateGroup(context.Background(), 12, map[string]any{"name": "Translators"})
	if err != nil || g.Name != "Translators" {
		t.Errorf("UpdateGroup = %v, %v", g, err)
	}
}

func TestDeleteGroupDeletesTheGroup(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := newTestClient(t, srv.URL).DeleteGroup(context.Background(), 12); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	if got != "DELETE /api/groups/12/" {
		t.Errorf("request = %s", got)
	}
}

func TestProjectURLIsAbsolute(t *testing.T) {
	c := newTestClient(t, "https://example.com/weblate")
	if got, want := c.ProjectURL("docs"), "https://example.com/weblate/api/projects/docs/"; got != want {
		t.Errorf("ProjectURL = %q, want %q", got, want)
	}
}

func TestProjectSlugFromURL(t *testing.T) {
	for _, tc := range []struct{ url, want string }{
		{"https://weblate.example/api/projects/docs/", "docs"},
		{"https://example.com/weblate/api/projects/docs/", "docs"},
		{"https://weblate.example/api/projects/docs", "docs"},
	} {
		if got, err := ProjectSlugFromURL(tc.url); err != nil || got != tc.want {
			t.Errorf("ProjectSlugFromURL(%q) = %q, %v, want %q", tc.url, got, err, tc.want)
		}
	}
	for _, bad := range []string{"https://weblate.example/api/groups/1/", "https://weblate.example/api/projects/", "::"} {
		if got, err := ProjectSlugFromURL(bad); err == nil {
			t.Errorf("ProjectSlugFromURL(%q) = %q, want an error", bad, got)
		}
	}
}
