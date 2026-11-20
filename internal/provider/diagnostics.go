package provider

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

// apiErrorDiagnostic turns an error from the client into an error diagnostic whose detail says what the user can do.
func apiErrorDiagnostic(summary string, err error) diag.Diagnostic {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return diag.NewErrorDiagnostic(summary, err.Error())
	}
	var detail string
	switch {
	case apiErr.NotJSON:
		detail = "Weblate's API answers with JSON, but this response is not JSON, so url does not point to a Weblate " +
			"instance. Check url in the provider block or the WEBLATE_URL environment variable."
	case apiErr.StatusCode == http.StatusUnauthorized:
		detail = "Weblate rejected the API token: it is invalid or expired. Check token in the provider block or the " +
			"WEBLATE_API_TOKEN environment variable."
	case apiErr.StatusCode == http.StatusForbidden:
		detail = "The API token lacks the permission for this object. A Project token can manage only objects inside " +
			"its Project."
	case apiErr.StatusCode == http.StatusTooManyRequests && apiErr.RetryAfter > 0:
		detail = fmt.Sprintf("Weblate's API rate limit for this user is used up, and Weblate asks to retry in %s. "+
			"The limit is set by WEBLATE_API_RATELIMIT_USER on the Weblate instance.", apiErr.RetryAfter)
	}
	if detail == "" {
		return diag.NewErrorDiagnostic(summary, err.Error())
	}
	return diag.NewErrorDiagnostic(summary, detail+"\n\n"+err.Error())
}
