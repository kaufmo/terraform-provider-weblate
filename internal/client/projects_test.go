package client

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	if *p != want {
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
