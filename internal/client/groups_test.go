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

func TestGetGroupDecodesTheGrants(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 12, "roles": ["https://weblate.example/api/roles/3/"],
			"languages": ["https://weblate.example/api/languages/de/"],
			"projects": ["https://weblate.example/api/projects/docs/"],
			"componentlists": ["https://weblate.example/api/component-lists/core/"],
			"components": ["https://weblate.example/api/components/docs/guides%252Fapi/"]}`))
	}))
	defer srv.Close()

	g, err := newTestClient(t, srv.URL).GetGroup(context.Background(), 12)
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	for grant, want := range map[GrantType]string{
		RoleGrants:          "https://weblate.example/api/roles/3/",
		LanguageGrants:      "https://weblate.example/api/languages/de/",
		ProjectGrants:       "https://weblate.example/api/projects/docs/",
		ComponentListGrants: "https://weblate.example/api/component-lists/core/",
		ComponentGrants:     "https://weblate.example/api/components/docs/guides%252Fapi/",
	} {
		if got := g.Grants(grant); len(got) != 1 || got[0] != want {
			t.Errorf("Grants(%s) = %v, want [%s]", grant.Endpoint, got, want)
		}
	}
}

func TestAddGrantPostsTheKeyThatWeblateExpects(t *testing.T) {
	for _, tc := range []struct {
		grant GrantType
		key   any
		path  string
		body  string
	}{
		{RoleGrants, int64(3), "/api/groups/12/roles/", `{"role_id":3}`},
		{LanguageGrants, "de", "/api/groups/12/languages/", `{"language_code":"de"}`},
		{ProjectGrants, int64(7), "/api/groups/12/projects/", `{"project_id":7}`},
		{ComponentListGrants, int64(2), "/api/groups/12/componentlists/", `{"component_list_id":2}`},
		{ComponentGrants, int64(9), "/api/groups/12/components/", `{"component_id":9}`},
	} {
		var got, body string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data, _ := io.ReadAll(r.Body)
			got, body = r.Method+" "+r.URL.Path, string(data)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": 12}`))
		}))
		if err := newTestClient(t, srv.URL).AddGrant(context.Background(), 12, tc.grant, tc.key); err != nil {
			t.Errorf("AddGrant(%s): %v", tc.grant.Endpoint, err)
		}
		srv.Close()
		if got != "POST "+tc.path || body != tc.body {
			t.Errorf("AddGrant(%s) sent %s %s, want POST %s %s", tc.grant.Endpoint, got, body, tc.path, tc.body)
		}
	}
}

func TestRemoveGrantDeletesTheKey(t *testing.T) {
	for _, tc := range []struct {
		grant GrantType
		key   any
		path  string
	}{
		{RoleGrants, int64(3), "/api/groups/12/roles/3/"},
		{LanguageGrants, "pt_BR", "/api/groups/12/languages/pt_BR/"},
		{ComponentListGrants, int64(2), "/api/groups/12/componentlists/2/"},
	} {
		var got string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Method + " " + r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		if err := newTestClient(t, srv.URL).RemoveGrant(context.Background(), 12, tc.grant, tc.key); err != nil {
			t.Errorf("RemoveGrant(%s): %v", tc.grant.Endpoint, err)
		}
		srv.Close()
		if got != "DELETE "+tc.path {
			t.Errorf("RemoveGrant(%s) sent %s, want DELETE %s", tc.grant.Endpoint, got, tc.path)
		}
	}
}

func TestGetComponentListReadsBySlug(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/component-lists/core/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 2, "slug": "core", "name": "Core"}`))
	}))
	defer srv.Close()

	l, err := newTestClient(t, srv.URL).GetComponentList(context.Background(), "core")
	if err != nil || l.ID != 2 {
		t.Errorf("GetComponentList = %v, %v, want ID 2", l, err)
	}
}

func TestKeyFromURL(t *testing.T) {
	for _, tc := range []struct{ url, collection, want string }{
		{"https://weblate.example/api/roles/3/", "roles", "3"},
		{"https://example.com/weblate/api/languages/sr@latin/", "languages", "sr@latin"},
		{"https://weblate.example/api/component-lists/core/", "component-lists", "core"},
	} {
		if got, err := KeyFromURL(tc.url, tc.collection); err != nil || got != tc.want {
			t.Errorf("KeyFromURL(%q) = %q, %v, want %q", tc.url, got, err, tc.want)
		}
	}
	if got, err := KeyFromURL("https://weblate.example/api/roles/3/", "languages"); err == nil {
		t.Errorf("KeyFromURL of a Role as a Language = %q, want an error", got)
	}
}
