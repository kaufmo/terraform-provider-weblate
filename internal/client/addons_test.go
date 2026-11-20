package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const alreadyInstalled = `{"type": "validation_error", "errors": [{"code": "invalid",
	"detail": "Add-on already installed: weblate.gettext.mo", "attr": "name"}]}`

func TestCreateAddonPostsToTheOwnerAndRecognizesAnInstalledAddon(t *testing.T) {
	var method, requestURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, requestURI = r.Method, r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(alreadyInstalled))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)

	_, err := c.CreateAddon(context.Background(), ComponentRef("docs/guides/app"), map[string]any{"name": "weblate.gettext.mo"})
	if method != http.MethodPost || requestURI != "/api/components/docs/guides%252Fapp/addons/" {
		t.Errorf("request = %s %s, want POST /api/components/docs/guides%%252Fapp/addons/", method, requestURI)
	}
	if !IsAddonAlreadyInstalled(err, "weblate.gettext.mo") {
		t.Errorf("IsAddonAlreadyInstalled(%v) = false, want true", err)
	}
	if IsAddonAlreadyInstalled(err, "weblate.gettext.linguas") {
		t.Errorf("IsAddonAlreadyInstalled for another name = true, want false")
	}
}

func TestFindAddonMatchesNameAndOwner(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.RequestURI() {
		case "/api/addons/":
			_, _ = w.Write([]byte(`{"next": "` + siteDomain + `/api/addons/?page=2", "results": [
				{"id": 1, "name": "weblate.gettext.mo", "project": null,
				 "component": "` + siteDomain + `/api/components/docs/other/"},
				{"id": 2, "name": "weblate.gettext.mo", "component": null,
				 "project": "` + siteDomain + `/api/projects/docs/"}]}`))
		case "/api/addons/?page=2":
			_, _ = w.Write([]byte(`{"next": null, "results": [
				{"id": 3, "name": "weblate.gettext.linguas", "project": null,
				 "component": "` + siteDomain + `/api/components/docs/guides%252Fapp/"},
				{"id": 4, "name": "weblate.gettext.mo", "project": null, "configuration": {"fuzzy": true},
				 "component": "` + siteDomain + `/api/components/docs/guides%252Fapp/"}]}`))
		default:
			t.Errorf("unexpected request %s", r.URL.RequestURI())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)

	for owner, want := range map[string]int64{ComponentRef("docs/guides/app"): 4, ProjectRef("docs"): 2} {
		a, err := c.FindAddon(context.Background(), owner, "weblate.gettext.mo")
		if err != nil {
			t.Fatalf("FindAddon(%s): %v", owner, err)
		}
		if a == nil || a.ID != want {
			t.Errorf("FindAddon(%s) = %+v, want the Add-on %d", owner, a, want)
		}
	}
	if a, err := c.FindAddon(context.Background(), ProjectRef("other"), "weblate.gettext.mo"); a != nil || err != nil {
		t.Errorf("FindAddon on another Project = %+v, %v, want nil", a, err)
	}
}

func TestComponentSlugPathFromURL(t *testing.T) {
	for u, want := range map[string]string{
		siteDomain + "/api/components/docs/app/":                       "docs/app",
		siteDomain + "/weblate/api/components/docs/guides%252Fapi%252Fapp/": "docs/guides/api/app",
	} {
		got, err := ComponentSlugPathFromURL(u)
		if err != nil || got != want {
			t.Errorf("ComponentSlugPathFromURL(%s) = %q, %v, want %q", u, got, err, want)
		}
	}
	if _, err := ComponentSlugPathFromURL(siteDomain + "/api/projects/docs/"); err == nil {
		t.Errorf("ComponentSlugPathFromURL of a Project URL: want an error")
	}
}
