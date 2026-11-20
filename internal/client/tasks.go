package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	pollInterval = 2 * time.Second
	// resendInterval is how long a deletion without a task waits before it sends DELETE again.
	resendInterval = time.Minute
)

// Task is a background task of Weblate, read from /api/tasks/{id}/.
type Task struct {
	Completed bool `json:"completed"`
	Progress  int  `json:"progress"`
	// Result is what the task returned. Weblate has no failure flag: a failed task is completed with the exception
	// message as a JSON string.
	Result json.RawMessage `json:"result"`
	Log    string          `json:"log"`
}

// Failure returns the exception message of a failed task, or "" when the task has not failed.
func (t *Task) Failure() string {
	var msg string
	if !t.Completed || json.Unmarshal(t.Result, &msg) != nil {
		return ""
	}
	return msg
}

// WaitForDeletion polls objectRef until it returns 404, which is the only sign that Weblate finished a deletion in
// the background. taskRef is the task URL from the DELETE response, or "" when Weblate returned none. A failed task
// ends the wait with an error. Without a task, as for a Category or Component, Weblate gives no sign when its removal
// task fails, for example on a lock that an Add-on holds, so the wait sends DELETE again once a minute; a second
// removal task finds nothing left to remove when the first one succeeded. When ctx ends first, the error names the
// task's progress; the task keeps running.
func (c *Client) WaitForDeletion(ctx context.Context, objectRef, taskRef string) error {
	progress := -1
	sinceDelete := time.Duration(0)
	for {
		err := c.Do(ctx, http.MethodGet, objectRef, nil, nil)
		if IsNotFound(err) {
			return nil
		}
		if ctx.Err() != nil {
			return waitTimeout("the deletion", progress)
		}
		if err != nil {
			return err
		}
		if taskRef != "" {
			var task Task
			err := c.Do(ctx, http.MethodGet, taskRef, nil, &task)
			switch {
			case IsNotFound(err):
				// Weblate keeps task metadata in a cache that can expire; the object's 404 remains the signal.
				taskRef = ""
			case ctx.Err() != nil:
				return waitTimeout("the deletion", progress)
			case err != nil:
				return err
			case task.Failure() != "":
				return fmt.Errorf("the deletion task failed: %s\n\nTask log:\n%s", task.Failure(), task.Log)
			default:
				progress = task.Progress
			}
		}
		if taskRef == "" && sinceDelete >= resendInterval {
			err := c.Do(ctx, http.MethodDelete, objectRef, nil, nil)
			switch {
			case ctx.Err() != nil:
				return waitTimeout("the deletion", progress)
			case err != nil && !IsNotFound(err):
				return err
			}
			sinceDelete = 0
		}
		if err := c.sleep(ctx, pollInterval); err != nil {
			return waitTimeout("the deletion", progress)
		}
		sinceDelete += pollInterval
	}
}

// WaitForTask polls the task at taskRef until it has completed. A failed task ends the wait with an error that holds
// the exception message and the task log. When ctx ends first, the error names the task's progress; the task keeps
// running.
func (c *Client) WaitForTask(ctx context.Context, taskRef string) error {
	progress := -1
	for {
		var task Task
		err := c.Do(ctx, http.MethodGet, taskRef, nil, &task)
		switch {
		case IsNotFound(err):
			// Weblate keeps task metadata in a cache that can expire, so a missing task has nothing left to wait for.
			return nil
		case ctx.Err() != nil:
			return waitTimeout("the background task", progress)
		case err != nil:
			return err
		case task.Failure() != "":
			return fmt.Errorf("the background task failed: %s\n\nTask log:\n%s", task.Failure(), task.Log)
		case task.Completed:
			return nil
		}
		progress = task.Progress
		if err := c.sleep(ctx, pollInterval); err != nil {
			return waitTimeout("the background task", progress)
		}
	}
}

func waitTimeout(what string, progress int) error {
	if progress < 0 {
		return fmt.Errorf("timed out waiting for Weblate to finish %s; it continues in the background", what)
	}
	return fmt.Errorf("timed out waiting for Weblate to finish %s; the task is at %d%% and continues in the "+
		"background", what, progress)
}
