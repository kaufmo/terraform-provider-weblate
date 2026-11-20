package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// deletionServer answers GET /api/projects/docs/ with 200 until gets reaches goneAfter, then with 404, and GET
// /api/tasks/1/ with task, or 404 when task is empty.
func deletionServer(t *testing.T, goneAfter int, task string) (*httptest.Server, *int) {
	t.Helper()
	gets := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/projects/docs/":
			gets++
			if gets > goneAfter {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"detail": "No Project matches the given query."}`))
				return
			}
			_, _ = w.Write([]byte(`{"slug": "docs"}`))
		case "/api/tasks/1/":
			if task == "" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"detail": "Invalid task"}`))
				return
			}
			_, _ = w.Write([]byte(task))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &gets
}

func noSleep(c *Client) *int {
	sleeps := 0
	c.sleep = func(ctx context.Context, _ time.Duration) error {
		sleeps++
		return ctx.Err()
	}
	return &sleeps
}

func TestWaitForDeletionPollsUntilNotFound(t *testing.T) {
	srv, gets := deletionServer(t, 2, `{"completed": false, "progress": 50, "result": null, "log": ""}`)
	c := newTestClient(t, srv.URL)
	sleeps := noSleep(c)

	if err := c.WaitForDeletion(context.Background(), "projects/docs/", "/api/tasks/1/"); err != nil {
		t.Fatalf("WaitForDeletion: %v", err)
	}
	if *gets != 3 || *sleeps != 2 {
		t.Errorf("GETs = %d, sleeps = %d, want 3 and 2", *gets, *sleeps)
	}
}

func TestWaitForDeletionReportsAFailedTask(t *testing.T) {
	srv, _ := deletionServer(t, 10,
		`{"completed": true, "progress": 100, "result": "Backup failed", "log": "Creating backup\nBackup failed"}`)
	c := newTestClient(t, srv.URL)
	noSleep(c)

	err := c.WaitForDeletion(context.Background(), "projects/docs/", "/api/tasks/1/")
	if err == nil {
		t.Fatal("WaitForDeletion succeeded, want the task failure")
	}
	for _, want := range []string{"Backup failed", "Creating backup"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestWaitForDeletionIgnoresAMissingTask(t *testing.T) {
	srv, gets := deletionServer(t, 1, "")
	c := newTestClient(t, srv.URL)
	noSleep(c)

	if err := c.WaitForDeletion(context.Background(), "projects/docs/", "/api/tasks/1/"); err != nil {
		t.Fatalf("WaitForDeletion: %v", err)
	}
	if *gets != 2 {
		t.Errorf("GETs = %d, want 2", *gets)
	}
}

func TestWaitForDeletionWithoutTask(t *testing.T) {
	srv, gets := deletionServer(t, 1, "")
	c := newTestClient(t, srv.URL)
	noSleep(c)

	if err := c.WaitForDeletion(context.Background(), "projects/docs/", ""); err != nil {
		t.Fatalf("WaitForDeletion: %v", err)
	}
	if *gets != 2 {
		t.Errorf("GETs = %d, want 2", *gets)
	}
}

func TestWaitForDeletionReportsProgressOnTimeout(t *testing.T) {
	srv, _ := deletionServer(t, 100, `{"completed": false, "progress": 40, "result": null, "log": ""}`)
	c := newTestClient(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	sleeps := 0
	c.sleep = func(context.Context, time.Duration) error {
		if sleeps++; sleeps == 2 {
			cancel()
			return context.DeadlineExceeded
		}
		return nil
	}

	err := c.WaitForDeletion(ctx, "projects/docs/", "/api/tasks/1/")
	if err == nil || !strings.Contains(err.Error(), "40%") {
		t.Errorf("WaitForDeletion error = %v, want one with the progress 40%%", err)
	}
}

// TestWaitForDeletionResendsTheDeleteWithoutTask covers deletions that return no task URL, such as of a Component:
// Weblate's removal task can fail without any sign, so the wait sends DELETE again once a minute.
func TestWaitForDeletionResendsTheDeleteWithoutTask(t *testing.T) {
	for name, tc := range map[string]struct {
		taskRef     string
		wantDeletes int
	}{
		"without task": {"", 2},
		"with task":    {"/api/tasks/1/", 0},
	} {
		t.Run(name, func(t *testing.T) {
			gets, deletes := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodDelete:
					deletes++
					w.WriteHeader(http.StatusNoContent)
				case r.URL.Path == "/api/tasks/1/":
					_, _ = w.Write([]byte(`{"completed": false, "progress": 10, "result": null, "log": ""}`))
				default:
					if gets++; gets > 65 {
						w.WriteHeader(http.StatusNotFound)
						_, _ = w.Write([]byte(`{"detail": "Not found."}`))
						return
					}
					_, _ = w.Write([]byte(`{"slug": "test"}`))
				}
			}))
			t.Cleanup(srv.Close)
			c := newTestClient(t, srv.URL)
			noSleep(c)

			if err := c.WaitForDeletion(context.Background(), "components/docs/test/", tc.taskRef); err != nil {
				t.Fatalf("WaitForDeletion: %v", err)
			}
			if deletes != tc.wantDeletes {
				t.Errorf("DELETEs = %d, want %d", deletes, tc.wantDeletes)
			}
		})
	}
}
