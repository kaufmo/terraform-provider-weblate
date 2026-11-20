package client

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"
)

// APIError is a response that the client could not use: a status outside 2xx, or a body that is not JSON.
type APIError struct {
	Method     string
	URL        string
	StatusCode int
	// Body is the response body when it is JSON, such as Weblate's {"detail": "..."}.
	Body string
	// NotJSON is set when the response is not JSON, so it most likely does not come from Weblate's API.
	NotJSON bool
	// RetryAfter is the wait that Weblate demanded with a 429 response.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("%s %s returned %d", e.Method, e.URL, e.StatusCode)
	switch {
	case e.NotJSON:
		return msg + " with a response that is not JSON"
	case e.Body != "":
		return msg + ": " + e.Body
	}
	return msg
}

// IsNotFound reports whether err is a 404 response from Weblate's API.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound && !apiErr.NotJSON
}

func isJSON(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && (mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"))
}
