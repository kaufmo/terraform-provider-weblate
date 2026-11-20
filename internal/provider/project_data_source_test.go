package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

// readProject runs the weblate_project data source for slug against a test server that answers with status and body.
func readProject(t *testing.T, slug string, status int, body string) datasource.ReadResponse {
	t.Helper()
	return readDataSource(t, NewProjectDataSource(), "/api/projects/"+slug+"/", "slug", slug, status, body)
}

// readDataSource runs ds with the one required attribute set to value against a test server that expects a GET on
// apiPath and answers with status and body.
func readDataSource(t *testing.T, ds datasource.DataSource, apiPath, attribute, value string, status int, body string) datasource.ReadResponse {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != apiPath {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL, "wlu_test")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	var configureResp datasource.ConfigureResponse
	ds.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: c}, &configureResp)
	var schemaResp datasource.SchemaResponse
	ds.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)

	objectType := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	values := map[string]tftypes.Value{}
	for name, typ := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(typ, nil)
	}
	values[attribute] = tftypes.NewValue(tftypes.String, value)
	config := tfsdk.Config{Schema: schemaResp.Schema, Raw: tftypes.NewValue(objectType, values)}

	resp := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(objectType, nil)}}
	ds.Read(ctx, datasource.ReadRequest{Config: config}, &resp)
	return resp
}

func TestProjectDataSourceReadsTheProject(t *testing.T) {
	resp := readProject(t, "docs", http.StatusOK, `{"id": 7, "slug": "docs", "name": "Docs",
		"web": "https://docs.example.com", "instructions": "Read the style guide.", "workspace": null,
		"check_flags": "", "translation_review": true, "source_review": false, "commit_policy": 30,
		"enable_hooks": true, "language_aliases": "", "access_control": 100, "public_sharing": false,
		"use_shared_tm": true, "contribute_shared_tm": true, "use_workspace_tm": false,
		"contribute_workspace_tm": false, "autoclean_tm": false, "enforced_2fa": true}`)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}

	ctx := context.Background()
	for name, want := range map[string]any{
		"id":                 types.StringValue("docs"),
		"weblate_id":         types.Int64Value(7),
		"name":               types.StringValue("Docs"),
		"web":                types.StringValue("https://docs.example.com"),
		"instructions":       types.StringValue("Read the style guide."),
		"workspace":          types.StringNull(),
		"translation_review": types.BoolValue(true),
		"commit_policy":      types.Int64Value(30),
		"access_control":     types.Int64Value(100),
		"enforced_2fa":       types.BoolValue(true),
	} {
		var got any
		switch want.(type) {
		case types.String:
			var v types.String
			resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root(name), &v)...)
			got = v
		case types.Int64:
			var v types.Int64
			resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root(name), &v)...)
			got = v
		case types.Bool:
			var v types.Bool
			resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root(name), &v)...)
			got = v
		}
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	if resp.Diagnostics.HasError() {
		t.Fatalf("reading the state: %v", resp.Diagnostics)
	}
}

func TestProjectDataSourceSetsWorkspace(t *testing.T) {
	resp := readProject(t, "docs", http.StatusOK,
		`{"id": 7, "slug": "docs", "workspace": "5f1c5bd6-3d0e-4a8b-9a4f-1c2d3e4f5a6b"}`)
	var workspace types.String
	resp.Diagnostics.Append(resp.State.GetAttribute(context.Background(), path.Root("workspace"), &workspace)...)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	if workspace.ValueString() != "5f1c5bd6-3d0e-4a8b-9a4f-1c2d3e4f5a6b" {
		t.Errorf("workspace = %v", workspace)
	}
}

func TestProjectDataSourceNamesTheMissingSlug(t *testing.T) {
	resp := readProject(t, "gone", http.StatusNotFound, `{"detail": "No Project matches the given query."}`)
	errs := resp.Diagnostics.Errors()
	if len(errs) != 1 {
		t.Fatalf("diagnostics = %v, want one error", resp.Diagnostics)
	}
	if !strings.Contains(errs[0].Summary()+errs[0].Detail(), `"gone"`) {
		t.Errorf("error %q / %q does not name the slug", errs[0].Summary(), errs[0].Detail())
	}
}

func TestProviderRegistersProjectDataSource(t *testing.T) {
	ctx := context.Background()
	for _, factory := range New("test")().DataSources(ctx) {
		var resp datasource.MetadataResponse
		factory().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "weblate"}, &resp)
		if resp.TypeName == "weblate_project" {
			return
		}
	}
	t.Error("the provider has no weblate_project data source")
}
