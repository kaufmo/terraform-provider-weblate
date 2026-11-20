package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// scripted answers each request with the next status in statuses and records the request bodies. A status of 0 drops
// the connection without a response. Drop it only on the first request: on a reused connection, Go's transport retries
// a GET by itself and the client never sees the error.
type scripted struct {
	statuses   []int
	retryAfter string
	bodies     []string
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.bodies = append(s.bodies, string(body))
	status := s.statuses[min(len(s.bodies), len(s.statuses))-1]
	if status == 0 {
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
		return
	}
	if s.retryAfter != "" {
		w.Header().Set("Retry-After", s.retryAfter)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{}`))
}

// run sends one request through a client that records its waits instead of sleeping.
func (s *scripted) run(t *testing.T, method string) ([]time.Duration, error) {
	t.Helper()
	srv := httptest.NewServer(s)
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	var waits []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	err := c.Do(context.Background(), method, "projects/", map[string]string{"slug": "docs"}, nil)
	return waits, err
}

func wantStatus(t *testing.T, err error, status int) *APIError {
	t.Helper()
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
		t.Fatalf("err = %v, want *APIError with status %d", err, status)
	}
	return apiErr
}

func TestRetriesTransientErrorsWithBackoff(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			s := &scripted{statuses: []int{0, 502, 503, 504, 200}}
			waits, err := s.run(t, method)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
			if !reflect.DeepEqual(waits, want) {
				t.Errorf("waits = %v, want %v", waits, want)
			}
			if s.bodies[4] != `{"slug":"docs"}` {
				t.Errorf("body of the last attempt = %q", s.bodies[4])
			}
		})
	}
}

func TestGivesUpAfterFiveAttempts(t *testing.T) {
	s := &scripted{statuses: []int{503}}
	_, err := s.run(t, http.MethodGet)
	wantStatus(t, err, 503)
	if len(s.bodies) != 5 {
		t.Errorf("attempts = %d, want 5", len(s.bodies))
	}
}

func TestPostIsNotRetriedOnTransientErrors(t *testing.T) {
	for _, status := range []int{502, 503, 504} {
		s := &scripted{statuses: []int{status, 201}}
		_, err := s.run(t, http.MethodPost)
		wantStatus(t, err, status)
		if len(s.bodies) != 1 {
			t.Errorf("%d: attempts = %d, want 1", status, len(s.bodies))
		}
	}

	s := &scripted{statuses: []int{0, 201}}
	if _, err := s.run(t, http.MethodPost); err == nil || len(s.bodies) != 1 {
		t.Errorf("connection error: err = %v, attempts = %d, want an error after 1 attempt", err, len(s.bodies))
	}
}

func TestRateLimitIsRetriedForEveryMethod(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		s := &scripted{statuses: []int{429, 201}, retryAfter: "60"}
		waits, err := s.run(t, method)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if !reflect.DeepEqual(waits, []time.Duration{60 * time.Second}) {
			t.Errorf("%s: waits = %v, want the Retry-After of 60s", method, waits)
		}
	}
}

func TestRateLimitWithoutRetryAfterUsesBackoff(t *testing.T) {
	s := &scripted{statuses: []int{429, 200}}
	waits, err := s.run(t, http.MethodGet)
	if err != nil || !reflect.DeepEqual(waits, []time.Duration{time.Second}) {
		t.Errorf("err = %v, waits = %v", err, waits)
	}
}

func TestRateLimitAboveOneMinuteFailsAtOnce(t *testing.T) {
	s := &scripted{statuses: []int{429, 200}, retryAfter: "61"}
	waits, err := s.run(t, http.MethodGet)
	apiErr := wantStatus(t, err, 429)
	if apiErr.RetryAfter != 61*time.Second {
		t.Errorf("RetryAfter = %v", apiErr.RetryAfter)
	}
	if len(waits) != 0 || len(s.bodies) != 1 {
		t.Errorf("waits = %v, attempts = %d, want no retry", waits, len(s.bodies))
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error %q", err)
	}
}

func TestCanceledContextStopsRetries(t *testing.T) {
	srv := httptest.NewServer(&scripted{statuses: []int{503}})
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	c.sleep = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}
	if err := c.Do(ctx, http.MethodGet, "projects/", nil, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
