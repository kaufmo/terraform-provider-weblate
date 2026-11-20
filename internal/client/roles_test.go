package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestListRolesReadsEveryPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.RequestURI() {
		case "/api/roles/":
			_, _ = w.Write([]byte(`{"count": 2, "next": "` + siteDomain + `/api/roles/?page=2", "previous": null,
				"results": [{"id": 1, "name": "Administration", "permissions": ["project.edit", "vcs.view"],
				"url": "` + siteDomain + `/api/roles/1/"}]}`))
		case "/api/roles/?page=2":
			_, _ = w.Write([]byte(`{"count": 2, "next": null, "previous": "` + siteDomain + `/api/roles/",
				"results": [{"id": 4, "name": "Translate", "permissions": [], "url": "` + siteDomain + `/api/roles/4/"}]}`))
		default:
			t.Errorf("unexpected request %s", r.URL.RequestURI())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv.URL).ListRoles(context.Background())
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	want := []Role{
		{ID: 1, Name: "Administration", Permissions: []string{"project.edit", "vcs.view"}},
		{ID: 4, Name: "Translate", Permissions: []string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListRoles = %+v, want %+v", got, want)
	}
}
