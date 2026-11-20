package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

const storedProject = `{"id": 7, "slug": "docs", "name": "Docs", "web": "https://docs.example.com",
	"instructions": "Read the style guide.", "workspace": null, "check_flags": "", "translation_review": true,
	"source_review": false, "commit_policy": 0, "enable_hooks": true, "language_aliases": "", "access_control": 0,
	"public_sharing": false, "use_shared_tm": true, "contribute_shared_tm": true, "use_workspace_tm": false,
	"contribute_workspace_tm": false, "autoclean_tm": false, "enforced_2fa": false}`

// request is one request that the fake Weblate received.
type request struct {
	method, path string
	body         map[string]any
}

// fakeWeblate answers each request with the response that respond returns for it and records the requests.
func fakeWeblate(t *testing.T, respond func(r request) (int, string)) (*client.Client, *[]request) {
	t.Helper()
	var requests []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := request{method: r.Method, path: r.URL.Path}
		if data, _ := io.ReadAll(r.Body); len(data) > 0 {
			if err := json.Unmarshal(data, &req.body); err != nil {
				t.Errorf("request body %s: %v", data, err)
			}
		}
		requests = append(requests, req)
		status, body := respond(req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL, "wlu_test")
	if err != nil {
		t.Fatal(err)
	}
	return c, &requests
}

func projectResourceWith(c *client.Client) (resource.ResourceWithConfigure, resource.SchemaResponse) {
	ctx := context.Background()
	r := NewProjectResource().(resource.ResourceWithConfigure)
	r.Configure(ctx, resource.ConfigureRequest{ProviderData: c}, &resource.ConfigureResponse{})
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	return r, schemaResp
}

// projectValue returns a weblate_project object with the given attributes and null for all others.
func projectValue(s resource.SchemaResponse, attributes map[string]any) tftypes.Value {
	objectType := s.Schema.Type().TerraformType(context.Background()).(tftypes.Object)
	values := map[string]tftypes.Value{}
	for name, typ := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(typ, attributes[name])
	}
	return tftypes.NewValue(objectType, values)
}

func stateAttribute[T any](t *testing.T, state tfsdk.State, name string) T {
	t.Helper()
	var v T
	if diags := state.GetAttribute(context.Background(), path.Root(name), &v); diags.HasError() {
		t.Fatalf("reading %s from state: %v", name, diags)
	}
	return v
}

func TestProjectCreateSendsOnlyConfiguredAttributes(t *testing.T) {
	c, requests := fakeWeblate(t, func(request) (int, string) { return http.StatusCreated, storedProject })
	r, s := projectResourceWith(c)
	config := map[string]any{"name": "Docs", "slug": "docs", "web": "https://docs.example.com",
		"instructions": "Read the style guide.\n", "translation_review": true}

	resp := resource.CreateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, nil)}}
	r.Create(context.Background(), resource.CreateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, config)},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}

	if len(*requests) != 1 || (*requests)[0].method != http.MethodPost || (*requests)[0].path != "/api/projects/" {
		t.Fatalf("requests = %v, want one POST /api/projects/", *requests)
	}
	if got := (*requests)[0].body; len(got) != len(config) || got["instructions"] != "Read the style guide.\n" ||
		got["translation_review"] != true {
		t.Errorf("body = %v, want the configured attributes %v", got, config)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "docs" {
		t.Errorf("id = %v", id)
	}
	if id := stateAttribute[types.Int64](t, resp.State, "weblate_id"); id.ValueInt64() != 7 {
		t.Errorf("weblate_id = %v", id)
	}
	if v := stateAttribute[types.Int64](t, resp.State, "access_control"); v.ValueInt64() != 0 || v.IsNull() {
		t.Errorf("access_control = %v, want the stored 0", v)
	}
}

func TestProjectCreateFailsWhenWeblateStoresAnotherValue(t *testing.T) {
	c, _ := fakeWeblate(t, func(request) (int, string) { return http.StatusCreated, storedProject })
	r, s := projectResourceWith(c)
	config := map[string]any{"name": "Docs", "slug": "docs", "access_control": int64(100)}

	resp := resource.CreateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, nil)}}
	r.Create(context.Background(), resource.CreateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, config)},
	}, &resp)

	errs := resp.Diagnostics.Errors()
	if len(errs) != 1 || !strings.Contains(errs[0].Detail(), "access_control") {
		t.Fatalf("diagnostics = %v, want one error on access_control", resp.Diagnostics)
	}
	if id := stateAttribute[types.Int64](t, resp.State, "weblate_id"); id.ValueInt64() != 7 {
		t.Errorf("weblate_id = %v, want the created Project in state", id)
	}
}

func readProjectResource(t *testing.T, respond func(r request) (int, string), state map[string]any) resource.ReadResponse {
	t.Helper()
	c, _ := fakeWeblate(t, respond)
	r, s := projectResourceWith(c)
	resp := resource.ReadResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, state)}}
	r.Read(context.Background(), resource.ReadRequest{State: resp.State}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	return resp
}

func TestProjectReadFindsARenamedProjectByWeblateID(t *testing.T) {
	resp := readProjectResource(t, func(r request) (int, string) {
		if r.path == "/api/projects/docs/" {
			return http.StatusNotFound, `{"detail": "No Project matches the given query."}`
		}
		return http.StatusOK, `{"next": null, "results": [{"id": 3, "slug": "other"},
			{"id": 7, "slug": "renamed", "name": "Renamed"}]}`
	}, map[string]any{"id": "docs", "slug": "docs", "weblate_id": int64(7)})

	if slug := stateAttribute[trimmedString](t, resp.State, "slug"); slug.ValueString() != "renamed" {
		t.Errorf("slug = %v, want renamed", slug)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "renamed" {
		t.Errorf("id = %v, want renamed", id)
	}
}

func TestProjectReadRemovesADeletedProject(t *testing.T) {
	resp := readProjectResource(t, func(r request) (int, string) {
		if r.path == "/api/projects/docs/" {
			return http.StatusNotFound, `{"detail": "No Project matches the given query."}`
		}
		return http.StatusOK, `{"next": null, "results": [{"id": 3, "slug": "other"}]}`
	}, map[string]any{"id": "docs", "slug": "docs", "weblate_id": int64(7)})

	if !resp.State.Raw.IsNull() {
		t.Errorf("state = %v, want the resource removed", resp.State.Raw)
	}
}

func TestProjectReadAfterImportUsesTheSlug(t *testing.T) {
	resp := readProjectResource(t, func(r request) (int, string) {
		if r.path != "/api/projects/docs/" {
			t.Errorf("path = %s", r.path)
		}
		return http.StatusOK, storedProject
	}, map[string]any{"id": "docs"})

	if name := stateAttribute[trimmedString](t, resp.State, "name"); name.ValueString() != "Docs" {
		t.Errorf("name = %v", name)
	}
}

func TestProjectUpdatePatchesChangedAttributesUnderTheOldSlug(t *testing.T) {
	c, requests := fakeWeblate(t, func(request) (int, string) {
		return http.StatusOK, strings.Replace(storedProject, `"slug": "docs"`, `"slug": "guides"`, 1)
	})
	r, s := projectResourceWith(c)
	prior := map[string]any{"id": "docs", "weblate_id": int64(7), "name": "Docs", "slug": "docs",
		"translation_review": true, "enable_hooks": true}
	config := map[string]any{"name": "Docs", "slug": "guides", "translation_review": true}
	plan := map[string]any{"id": "guides", "weblate_id": int64(7), "name": "Docs", "slug": "guides",
		"translation_review": true, "enable_hooks": true}

	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)}}
	r.Update(context.Background(), resource.UpdateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, plan)},
		State:  tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}

	if len(*requests) != 1 || (*requests)[0].method != http.MethodPatch || (*requests)[0].path != "/api/projects/docs/" {
		t.Fatalf("requests = %v, want one PATCH /api/projects/docs/", *requests)
	}
	if got := (*requests)[0].body; len(got) != 1 || got["slug"] != "guides" {
		t.Errorf("body = %v, want only the new slug", got)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "guides" {
		t.Errorf("id = %v, want guides", id)
	}
}

// The polling itself is tested in the client; this test checks that Delete waits for the 404 at all.
func TestProjectDeleteWaitsUntilTheProjectIsGone(t *testing.T) {
	c, requests := fakeWeblate(t, func(r request) (int, string) {
		if r.method == http.MethodDelete {
			return http.StatusAccepted, `{"detail": "Project deletion scheduled.", "task_url": "/api/tasks/1/"}`
		}
		return http.StatusNotFound, `{"detail": "No Project matches the given query."}`
	})
	r, s := projectResourceWith(c)
	state := tfsdk.State{Schema: s.Schema, Raw: projectValue(s, map[string]any{"id": "docs", "slug": "docs"})}

	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}
	want := []request{{method: http.MethodDelete, path: "/api/projects/docs/"}, {method: http.MethodGet, path: "/api/projects/docs/"}}
	if len(*requests) != 2 || (*requests)[0].method != want[0].method || (*requests)[1].method != want[1].method ||
		(*requests)[1].path != want[1].path {
		t.Errorf("requests = %v, want %v", *requests, want)
	}
}

func TestProjectPlanSetsIDToTheNewSlug(t *testing.T) {
	r, s := projectResourceWith(nil)
	plan := tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, map[string]any{"id": tftypes.UnknownValue,
		"slug": "guides"})}
	resp := resource.ModifyPlanResponse{Plan: plan}
	r.(resource.ResourceWithModifyPlan).ModifyPlan(context.Background(), resource.ModifyPlanRequest{Plan: plan}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("ModifyPlan: %v", resp.Diagnostics)
	}
	var id types.String
	resp.Diagnostics.Append(resp.Plan.GetAttribute(context.Background(), path.Root("id"), &id)...)
	if id.ValueString() != "guides" {
		t.Errorf("planned id = %v, want guides", id)
	}
}

// The acceptance tests cannot run everywhere, so this catches schema mistakes that only the framework's own checks
// find, such as a plan modifier on an attribute that is not Computed.
func TestProviderSchemasAreValid(t *testing.T) {
	server, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		t.Errorf("%s: %s", d.Summary, d.Detail)
	}
}

func TestProviderRegistersProjectResource(t *testing.T) {
	ctx := context.Background()
	for _, factory := range New("test")().Resources(ctx) {
		var resp resource.MetadataResponse
		factory().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "weblate"}, &resp)
		if resp.TypeName == "weblate_project" {
			return
		}
	}
	t.Error("the provider has no weblate_project resource")
}
