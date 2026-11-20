package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

func TestSweepProjectsDeletesOnlyTestProjects(t *testing.T) {
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"detail": "Project deletion scheduled."}`))
		case r.URL.Query().Get("page") == "2":
			_, _ = w.Write([]byte(`{"next": null, "results": [{"slug": "tf-acc-2"}, {"slug": "docs-tf-acc-3"}]}`))
		default:
			_, _ = w.Write([]byte(`{"next": "http://localhost/api/projects/?page=2",
				"results": [{"slug": "docs"}, {"slug": "tf-acc-1"}]}`))
		}
	}))
	defer srv.Close()
	c, err := client.New(srv.URL, "wlu_test")
	if err != nil {
		t.Fatal(err)
	}

	if err := sweepProjects(context.Background(), c); err != nil {
		t.Fatalf("sweepProjects: %v", err)
	}
	if want := []string{"/api/projects/tf-acc-1/", "/api/projects/tf-acc-2/"}; !slices.Equal(deleted, want) {
		t.Errorf("deleted %v, want %v", deleted, want)
	}
}
