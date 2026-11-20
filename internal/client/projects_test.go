package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGetProjectDecodesTheProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects/docs/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 7, "slug": "docs", "name": "Docs", "web": "https://docs.example.com",
			"workspace": null, "commit_policy": 30, "access_control": 100, "use_shared_tm": true}`))
	}))
	defer srv.Close()

	p, err := newTestClient(t, srv.URL).GetProject(context.Background(), "docs")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	want := Project{ID: 7, Slug: "docs", Name: "Docs", Web: "https://docs.example.com", CommitPolicy: 30,
		AccessControl: 100, UseSharedTM: true}
	if got := string(p.Fields["commit_policy"]); got != "30" {
		t.Errorf("Fields[commit_policy] = %s, want the raw JSON 30", got)
	}
	if p.Fields = nil; !reflect.DeepEqual(*p, want) {
		t.Errorf("GetProject = %+v, want %+v", *p, want)
	}
}

func TestGetProjectReportsNotFound(t *testing.T) {
	srv := httptest.NewServer(respond(http.StatusNotFound, "application/json", `{"detail": "No Project matches the given query."}`))
	defer srv.Close()

	if _, err := newTestClient(t, srv.URL).GetProject(context.Background(), "gone"); !IsNotFound(err) {
		t.Errorf("GetProject error = %v, want a 404", err)
	}
}

func TestCreateProjectPostsTheFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/api/projects/" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if string(body) != `{"name":"Docs","slug":"docs","translation_review":true}` {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 7, "slug": "docs", "name": "Docs", "translation_review": true}`))
	}))
	defer srv.Close()

	p, err := newTestClient(t, srv.URL).CreateProject(context.Background(),
		map[string]any{"name": "Docs", "slug": "docs", "translation_review": true})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.ID != 7 || !p.TranslationReview {
		t.Errorf("CreateProject = %+v", *p)
	}
}

func TestUpdateProjectPatchesTheOldSlug(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPatch || r.URL.Path != "/api/projects/docs/" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if string(body) != `{"slug":"guides"}` {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 7, "slug": "guides"}`))
	}))
	defer srv.Close()

	p, err := newTestClient(t, srv.URL).UpdateProject(context.Background(), "docs", map[string]any{"slug": "guides"})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if p.Slug != "guides" {
		t.Errorf("UpdateProject = %+v", *p)
	}
}

func TestFindProjectByIDPagesThroughTheList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"next": null, "results": [{"id": 7, "slug": "renamed"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"next": "http://other.example/api/projects/?page=2", "results": [{"id": 1, "slug": "a"}]}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)

	p, err := c.FindProjectByID(context.Background(), 7)
	if err != nil || p == nil || p.Slug != "renamed" {
		t.Errorf("FindProjectByID(7) = %v, %v, want the Project renamed", p, err)
	}
	if p, err := c.FindProjectByID(context.Background(), 8); err != nil || p != nil {
		t.Errorf("FindProjectByID(8) = %v, %v, want nil, nil", p, err)
	}
}

func TestDeleteProjectReturnsTheTaskURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/projects/docs/" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"detail": "Project deletion scheduled.", "task_url": "/api/tasks/1/"}`))
	}))
	defer srv.Close()

	task, err := newTestClient(t, srv.URL).DeleteProject(context.Background(), "docs")
	if err != nil || task != "/api/tasks/1/" {
		t.Errorf("DeleteProject = %q, %v", task, err)
	}
}
