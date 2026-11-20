package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

var (
	null    = tftypes.NewValue(tftypes.String, nil)
	unknown = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
)

func str(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }

func configure(t *testing.T, url, token tftypes.Value) provider.ConfigureResponse {
	t.Helper()
	ctx := context.Background()
	p := New("test")()
	var schemaResp provider.SchemaResponse
	p.Schema(ctx, provider.SchemaRequest{}, &schemaResp)
	raw := tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), map[string]tftypes.Value{"url": url, "token": token})
	var resp provider.ConfigureResponse
	p.Configure(ctx, provider.ConfigureRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: raw}}, &resp)
	return resp
}

// tokenSeenBy sends a request with the client that Configure built and returns the Authorization header that the
// test server received.
func tokenSeenBy(t *testing.T, resp provider.ConfigureResponse, received *string) string {
	t.Helper()
	if resp.Diagnostics.HasError() {
		t.Fatalf("Configure: %v", resp.Diagnostics)
	}
	c, ok := resp.ResourceData.(*client.Client)
	if !ok || resp.DataSourceData != resp.ResourceData {
		t.Fatalf("ResourceData = %T, DataSourceData = %T, want the same *client.Client", resp.ResourceData, resp.DataSourceData)
	}
	if err := c.Do(context.Background(), http.MethodGet, "projects/", nil, nil); err != nil {
		t.Fatalf("request with the configured client: %v", err)
	}
	return *received
}

// weblate starts a test server and returns it with the Authorization header of its last request.
func weblate(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var authorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, &authorization
}

func wantAttributeError(t *testing.T, diags diag.Diagnostics, attribute string) {
	t.Helper()
	for _, d := range diags.Errors() {
		if withPath, ok := d.(diag.DiagnosticWithPath); ok && withPath.Path().Equal(path.Root(attribute)) {
			return
		}
	}
	t.Errorf("no error for %q in %v", attribute, diags)
}

func TestConfigureFallsBackToEnvironment(t *testing.T) {
	srv, received := weblate(t)
	t.Setenv("WEBLATE_URL", srv.URL)
	t.Setenv("WEBLATE_API_TOKEN", "wlu_env")

	if got := tokenSeenBy(t, configure(t, null, null), received); got != "Token wlu_env" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestConfigurePrefersTheProviderBlock(t *testing.T) {
	srv, received := weblate(t)
	t.Setenv("WEBLATE_URL", "ftp://not-used.example.com")
	t.Setenv("WEBLATE_API_TOKEN", "wlu_env")

	if got := tokenSeenBy(t, configure(t, str(srv.URL+"/"), str("wlu_block")), received); got != "Token wlu_block" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestConfigureRejectsMissingAndUnknownValues(t *testing.T) {
	t.Setenv("WEBLATE_URL", "")
	t.Setenv("WEBLATE_API_TOKEN", "")

	resp := configure(t, null, str(""))
	wantAttributeError(t, resp.Diagnostics, "url")
	wantAttributeError(t, resp.Diagnostics, "token")

	t.Setenv("WEBLATE_URL", "https://weblate.example.com")
	t.Setenv("WEBLATE_API_TOKEN", "wlu_env")
	resp = configure(t, unknown, unknown)
	wantAttributeError(t, resp.Diagnostics, "url")
	wantAttributeError(t, resp.Diagnostics, "token")
}

func TestConfigureRejectsInvalidURLs(t *testing.T) {
	for _, u := range []string{"ftp://weblate.example.com", "weblate.example.com", "https://weblate.example.com/api/"} {
		resp := configure(t, str(u), str("wlu_token"))
		wantAttributeError(t, resp.Diagnostics, "url")
	}
}
