package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func respond(status int, contentType, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func doGet(t *testing.T, h http.Handler, out any) error {
	t.Helper()
	srv := httptest.NewServer(h)
	defer srv.Close()
	return newTestClient(t, srv.URL).Do(context.Background(), http.MethodGet, "projects/docs/", nil, out)
}

func TestNotFoundIsAnAPIError(t *testing.T) {
	err := doGet(t, respond(http.StatusNotFound, "application/json", `{"detail":"No Project matches the given query."}`), &project{})

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("err = %v, want *APIError with status 404", err)
	}
	if !IsNotFound(err) {
		t.Error("IsNotFound = false")
	}
	if !strings.Contains(err.Error(), "No Project matches the given query.") {
		t.Errorf("error %q lacks the Weblate message", err)
	}
}

func TestNonJSONResponseIsNotWeblate(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"404 page": respond(http.StatusNotFound, "text/html", "<html>Not Found</html>"),
		"200 page": respond(http.StatusOK, "text/html; charset=utf-8", "<html>Welcome</html>"),
	} {
		t.Run(name, func(t *testing.T) {
			err := doGet(t, h, &project{})
			var apiErr *APIError
			if !errors.As(err, &apiErr) || !apiErr.NotJSON {
				t.Fatalf("err = %v, want *APIError with NotJSON", err)
			}
			if IsNotFound(err) {
				t.Error("IsNotFound = true for a response that does not come from Weblate")
			}
		})
	}
}

func TestEmptySuccessNeedsNoBody(t *testing.T) {
	if err := doGet(t, respond(http.StatusNoContent, "", ""), nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestIsNotFoundIgnoresOtherErrors(t *testing.T) {
	if IsNotFound(errors.New("boom")) || IsNotFound(&APIError{StatusCode: http.StatusForbidden}) {
		t.Error("IsNotFound = true")
	}
}
