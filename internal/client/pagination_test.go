package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// Weblate builds absolute URLs from its configured site domain, which need not be the URL the provider talks to.
const siteDomain = "https://weblate.example.com"

func TestListFollowsNextAcrossPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.RequestURI() {
		case "/weblate/api/projects/":
			_, _ = w.Write([]byte(`{"count":3,"next":"` + siteDomain + `/weblate/api/projects/?page=2","previous":null,
				"results":[{"slug":"a"},{"slug":"b"}]}`))
		case "/weblate/api/projects/?page=2":
			_, _ = w.Write([]byte(`{"count":3,"next":null,"previous":"` + siteDomain + `/weblate/api/projects/",
				"results":[{"slug":"c"}]}`))
		default:
			t.Errorf("unexpected request %s", r.URL.RequestURI())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := List[project](context.Background(), newTestClient(t, srv.URL+"/weblate"), "projects/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []project{{"a"}, {"b"}, {"c"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestDoKeepsOnlyPathAndQueryOfAbsoluteURLs(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Host + r.URL.RequestURI()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	if err := c.Do(context.Background(), http.MethodGet, siteDomain+"/api/projects/docs/?format=json", nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if want := srv.Listener.Addr().String() + "/api/projects/docs/?format=json"; got != want {
		t.Errorf("request went to %q, want %q", got, want)
	}
}
