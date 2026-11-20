package provider

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

func TestAPIErrorDiagnostic(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want []string
	}{
		"401": {
			err:  &client.APIError{StatusCode: 401, Body: `{"detail":"Invalid token."}`},
			want: []string{"invalid or expired", "WEBLATE_API_TOKEN"},
		},
		"403": {
			err:  &client.APIError{StatusCode: 403, Body: `{"detail":"Can not manage groups"}`},
			want: []string{"lacks the permission", "Can not manage groups"},
		},
		"not JSON": {
			err:  &client.APIError{StatusCode: 200, NotJSON: true},
			want: []string{"does not point to a Weblate instance", "WEBLATE_URL"},
		},
		"rate limit": {
			err:  &client.APIError{StatusCode: 429, RetryAfter: 2 * time.Hour},
			want: []string{"2h0m0s", "WEBLATE_API_RATELIMIT_USER"},
		},
		"other API error": {
			err:  &client.APIError{Method: "POST", URL: "https://weblate.example.com/api/projects/", StatusCode: 400, Body: `{"slug":["already exists"]}`},
			want: []string{"400", "already exists"},
		},
		"connection error": {
			err:  errors.New("dial tcp: connection refused"),
			want: []string{"connection refused"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := apiErrorDiagnostic("Could not read the Project", tc.err)
			if d.Summary() != "Could not read the Project" {
				t.Errorf("summary = %q", d.Summary())
			}
			for _, w := range tc.want {
				if !strings.Contains(d.Detail(), w) {
					t.Errorf("detail %q lacks %q", d.Detail(), w)
				}
			}
		})
	}
}
