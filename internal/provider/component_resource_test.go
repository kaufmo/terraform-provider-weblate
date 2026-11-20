package provider

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

// storedReference is the Component docs/guides/api/reference, in the Category api of categoryTree.
const (
	storedReference = `{"id": 12, "slug": "reference", "name": "Reference",
		"project": {"id": 7, "slug": "docs", "url": "http://weblate.example/api/projects/docs/"},
		"category": "http://weblate.example/api/categories/9/", "source_language": {"code": "en", "name": "English"},
		"vcs": "git", "vcs_params": {}, "repo": "http://git.example/demo.git", "branch": "main", "push_branch": "",
		"filemask": "po/*.po", "file_format": "po", "file_format_params": {"po_line_wrap": 77}, "priority": 100,
		"edit_template": true, "git_export": "", "url": "http://weblate.example/api/components/docs/guides%252Fapi%252Freference/",
		"task_url": null}`
	referencePath = "/api/components/docs/guides%2Fapi%2Freference/"
)

// withTask returns body with task_url pointing to the task abc.
func withTask(body string) string {
	return strings.Replace(body, `"task_url": null`, `"task_url": "http://weblate.example/api/tasks/abc/"`, 1)
}

// referenceTree answers like categoryTree with the Component reference added, answered by component.
func referenceTree(component string) func(r request) (int, string) {
	tree := categoryTree(storedAPI)
	return func(r request) (int, string) {
		switch r.path {
		case referencePath:
			return http.StatusOK, component
		case "/api/projects/docs/components/":
			return http.StatusOK, `{"next": null, "results": [` + component + `]}`
		case "/api/tasks/abc/":
			return http.StatusOK, `{"completed": true, "progress": 100, "result": {"component": 12}, "log": ""}`
		}
		return tree(r)
	}
}

func componentResourceWith(c *client.Client) (resource.Resource, resource.SchemaResponse) {
	ctx := context.Background()
	r := NewComponentResource()
	r.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: c}, &resource.ConfigureResponse{})
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	return r, schemaResp
}

var referenceConfig = map[string]any{"project": "docs", "category": "docs/guides/api", "name": "Reference",
	"slug": "reference", "repo": "http://git.example/demo.git", "filemask": "po/*.po", "file_format": "po",
	"source_language": "en", "file_format_params": `{"po_line_wrap":77}`}

func createComponent(t *testing.T, respond func(r request) (int, string), config map[string]any) (resource.CreateResponse, []request) {
	t.Helper()
	c, requests := fakeWeblate(t, respond)
	r, s := componentResourceWith(c)
	// The framework hands Create a null state.
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s.Schema,
		Raw: tftypes.NewValue(s.Schema.Type().TerraformType(context.Background()), nil)}}
	r.Create(context.Background(), resource.CreateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, config)},
	}, &resp)
	return resp, *requests
}

func TestComponentCreateInACategoryWaitsForTheTask(t *testing.T) {
	tree := referenceTree(storedReference)
	resp, requests := createComponent(t, func(r request) (int, string) {
		if r.method == http.MethodPost {
			return http.StatusCreated, withTask(storedReference)
		}
		return tree(r)
	}, referenceConfig)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}

	var post *request
	polled := false
	for i, r := range requests {
		if r.method == http.MethodPost {
			post = &requests[i]
		}
		polled = polled || r.path == "/api/tasks/abc/"
	}
	if post == nil || post.path != "/api/projects/docs/components/" {
		t.Fatalf("requests = %v, want a POST /api/projects/docs/components/", requests)
	}
	body := post.body
	if body["disable_autoshare"] != true || body["category"] != "http://weblate.example/api/categories/9/" ||
		body["source_language"] != "en" || body["project"] != nil {
		t.Errorf("body = %v, want disable_autoshare, the Category URL, the language code and no project", body)
	}
	if params, ok := body["file_format_params"].(map[string]any); !ok || params["po_line_wrap"] != float64(77) {
		t.Errorf("file_format_params = %#v, want a JSON object", body["file_format_params"])
	}
	if !polled {
		t.Errorf("requests = %v, want a poll of the task", requests)
	}
	for name, want := range map[string]string{"id": "docs/guides/api/reference", "category": "docs/guides/api",
		"project": "docs", "branch": "main", "vcs": "git", "source_language": "en"} {
		var got attr.Value
		resp.Diagnostics.Append(resp.State.GetAttribute(context.Background(), path.Root(name), &got)...)
		if got.String() != strconv.Quote(want) {
			t.Errorf("%s = %v, want %s", name, got, want)
		}
	}
	if id := stateAttribute[types.Int64](t, resp.State, "weblate_id"); id.ValueInt64() != 12 {
		t.Errorf("weblate_id = %v, want 12", id)
	}
	if p := stateAttribute[types.Int64](t, resp.State, "priority"); p.ValueInt64() != 100 {
		t.Errorf("priority = %v, want the stored 100", p)
	}
}

func TestComponentCreateKeepsTheComponentWhenTheTaskFails(t *testing.T) {
	tree := referenceTree(storedReference)
	resp, _ := createComponent(t, func(r request) (int, string) {
		switch {
		case r.method == http.MethodPost:
			return http.StatusCreated, withTask(storedReference)
		case r.path == "/api/tasks/abc/":
			return http.StatusOK, `{"completed": true, "progress": 100, "result": "Failed to parse po/de.po",
				"log": "Parsing po/de.po"}`
		}
		return tree(r)
	}, referenceConfig)

	errs := resp.Diagnostics.Errors()
	if len(errs) != 1 || !strings.Contains(errs[0].Detail(), "Failed to parse po/de.po") ||
		!strings.Contains(errs[0].Detail(), "Parsing po/de.po") {
		t.Fatalf("diagnostics = %v, want one error with the task result and log", resp.Diagnostics)
	}
	if id := stateAttribute[types.Int64](t, resp.State, "weblate_id"); id.ValueInt64() != 12 {
		t.Errorf("weblate_id = %v, want the created Component in state", id)
	}
}

func TestComponentCreateReportsWeblatesRepositoryError(t *testing.T) {
	resp, _ := createComponent(t, func(r request) (int, string) {
		if r.method == http.MethodPost {
			return http.StatusBadRequest, `{"type": "validation_error", "errors": [{"code": "invalid",
				"detail": "Could not update repository: fatal: repository not found", "attr": "repo"}]}`
		}
		return referenceTree(storedReference)(r)
	}, referenceConfig)

	if errs := resp.Diagnostics.Errors(); len(errs) != 1 || !strings.Contains(errs[0].Detail(), "repository not found") {
		t.Fatalf("diagnostics = %v, want Weblate's error", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Errorf("state = %v, want none", resp.State.Raw)
	}
}

func readComponentResource(t *testing.T, respond func(r request) (int, string), state map[string]any) resource.ReadResponse {
	t.Helper()
	c, _ := fakeWeblate(t, respond)
	r, s := componentResourceWith(c)
	resp := resource.ReadResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, state)}}
	r.Read(context.Background(), resource.ReadRequest{State: resp.State}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	return resp
}

func TestComponentReadFindsARenamedComponentByWeblateID(t *testing.T) {
	tree := referenceTree(storedReference)
	resp := readComponentResource(t, func(r request) (int, string) {
		if r.path == "/api/components/docs/guides%2Fapi%2Fold/" {
			return http.StatusNotFound, notFound
		}
		return tree(r)
	}, map[string]any{"id": "docs/guides/api/old", "weblate_id": int64(12), "project": "docs"})

	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "docs/guides/api/reference" {
		t.Errorf("id = %v, want docs/guides/api/reference", id)
	}
	if v := stateAttribute[jsontypes.Normalized](t, resp.State, "file_format_params"); v.ValueString() != `{"po_line_wrap":77}` {
		t.Errorf("file_format_params = %v", v)
	}
}

func TestComponentReadRemovesADeletedComponent(t *testing.T) {
	resp := readComponentResource(t, func(r request) (int, string) {
		if r.path == "/api/projects/docs/components/" {
			return http.StatusOK, `{"next": null, "results": []}`
		}
		return http.StatusNotFound, notFound
	}, map[string]any{"id": "docs/reference", "weblate_id": int64(12), "project": "docs"})
	if !resp.State.Raw.IsNull() {
		t.Errorf("state = %v, want the resource removed", resp.State.Raw)
	}
}

func updateComponent(t *testing.T, respond func(r request) (int, string), prior, config map[string]any) (resource.UpdateResponse, []request) {
	t.Helper()
	c, requests := fakeWeblate(t, respond)
	r, s := componentResourceWith(c)
	plan := withAttributes(prior, map[string]any{"id": tftypes.UnknownValue})
	for k, v := range config {
		plan[k] = v
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, plan)}}
	r.Update(context.Background(), resource.UpdateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, plan)},
		State:  tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)},
	}, &resp)
	return resp, *requests
}

var referenceState = withAttributes(referenceConfig, map[string]any{"id": "docs/guides/api/reference",
	"weblate_id": int64(12), "branch": "main"})

func TestComponentUpdateRenamesWithAPatchOfTheOldPath(t *testing.T) {
	renamed := strings.Replace(storedReference, `"slug": "reference"`, `"slug": "ref"`, 1)
	tree := referenceTree(storedReference)
	resp, requests := updateComponent(t, func(r request) (int, string) {
		if r.method == http.MethodPatch {
			return http.StatusOK, renamed
		}
		return tree(r)
	}, referenceState, withAttributes(referenceConfig, map[string]any{"slug": "ref"}))
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	if p := patches(requests); len(p) != 1 || p[0].path != referencePath || len(p[0].body) != 1 || p[0].body["slug"] != "ref" {
		t.Errorf("requests = %v, want one PATCH of the old path with only slug", requests)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "docs/guides/api/ref" {
		t.Errorf("id = %v, want docs/guides/api/ref", id)
	}
}

func TestComponentUpdateMovesToTheProjectRoot(t *testing.T) {
	atRoot := strings.Replace(storedReference, `"category": "http://weblate.example/api/categories/9/"`, `"category": null`, 1)
	tree := referenceTree(storedReference)
	config := withAttributes(referenceConfig, map[string]any{"category": nil})
	resp, requests := updateComponent(t, func(r request) (int, string) {
		if r.method == http.MethodPatch {
			return http.StatusOK, atRoot
		}
		return tree(r)
	}, referenceState, config)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	if p := patches(requests); len(p) != 1 || len(p[0].body) != 1 || p[0].body["category"] != nil {
		t.Errorf("requests = %v, want one PATCH with category: null", requests)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "docs/reference" {
		t.Errorf("id = %v, want docs/reference", id)
	}
}

func TestComponentUpdateToAnotherProjectFailsWithTheReplaceHint(t *testing.T) {
	tree := referenceTree(storedReference)
	resp, requests := updateComponent(t, func(r request) (int, string) {
		if r.path == "/api/projects/other/components/" {
			return http.StatusOK, `{"next": null, "results": []}`
		}
		return tree(r)
	}, referenceState, withAttributes(referenceConfig, map[string]any{"project": "other", "category": nil}))

	if errs := resp.Diagnostics.Errors(); len(errs) != 1 || !strings.Contains(errs[0].Detail(), "-replace") {
		t.Fatalf("diagnostics = %v, want one error that points to -replace", resp.Diagnostics)
	}
	if p := patches(requests); len(p) != 0 {
		t.Errorf("Update sent %v", p)
	}
}

func TestComponentUpdateAfterAProjectRenameOnlyChangesState(t *testing.T) {
	renamed := strings.ReplaceAll(storedReference, `"slug": "docs"`, `"slug": "handbook"`)
	resp, requests := updateComponent(t, func(r request) (int, string) {
		switch r.path {
		case "/api/projects/handbook/components/":
			return http.StatusOK, `{"next": null, "results": [` + renamed + `]}`
		case "/api/categories/9/":
			return http.StatusOK, strings.ReplaceAll(storedAPI, "/projects/docs/", "/projects/handbook/")
		case "/api/categories/4/":
			return http.StatusOK, strings.ReplaceAll(storedGuides, "/projects/docs/", "/projects/handbook/")
		}
		return http.StatusNotFound, notFound
	}, referenceState, withAttributes(referenceConfig, map[string]any{"project": "handbook",
		"category": "handbook/guides/api"}))
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	if p := patches(requests); len(p) != 0 {
		t.Errorf("Update sent %v", p)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "handbook/guides/api/reference" {
		t.Errorf("id = %v, want handbook/guides/api/reference", id)
	}
}

func modifyComponentPlan(t *testing.T, state, plan map[string]any) resource.ModifyPlanResponse {
	t.Helper()
	r, s := componentResourceWith(nil)
	p := tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, plan)}
	st := tfsdk.State{Schema: s.Schema, Raw: projectValue(s, state)}
	if state == nil {
		st.Raw = tftypes.NewValue(s.Schema.Type().TerraformType(context.Background()), nil)
	}
	resp := resource.ModifyPlanResponse{Plan: p}
	r.(resource.ResourceWithModifyPlan).ModifyPlan(context.Background(),
		resource.ModifyPlanRequest{Plan: p, State: st}, &resp)
	return resp
}

func TestComponentPlanSetsIDToTheNewSlugPath(t *testing.T) {
	resp := modifyComponentPlan(t, nil, withAttributes(referenceConfig, map[string]any{"id": tftypes.UnknownValue}))
	var id types.String
	resp.Diagnostics.Append(resp.Plan.GetAttribute(context.Background(), path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() || id.ValueString() != "docs/guides/api/reference" {
		t.Errorf("planned id = %v (%v), want docs/guides/api/reference", id, resp.Diagnostics)
	}
}

func TestComponentPlanRejectsAnotherSourceLanguage(t *testing.T) {
	resp := modifyComponentPlan(t, referenceState, withAttributes(referenceState, map[string]any{"source_language": "de"}))
	errs := resp.Diagnostics.Errors()
	if len(errs) != 1 || !strings.Contains(errs[0].Detail(), "-replace") {
		t.Errorf("diagnostics = %v, want one error that points to -replace", resp.Diagnostics)
	}
}

func TestComponentPathsThatWeblateWouldCleanFailValidation(t *testing.T) {
	_, s := componentResourceWith(nil)
	for value, cleaned := range map[string]string{"./po/*.po": "po/*.po", "/po/*.po": "po/*.po",
		"po//*.po": "po/*.po", "po/../*.po": "po/*.po", "po/*.po": "", "": ""} {
		var diags []string
		for _, v := range s.Schema.Attributes["filemask"].(schema.StringAttribute).Validators {
			var resp validator.StringResponse
			v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("filemask"),
				ConfigValue: types.StringValue(value)}, &resp)
			for _, d := range resp.Diagnostics.Errors() {
				diags = append(diags, d.Detail())
			}
		}
		switch {
		case cleaned == "" && diags != nil:
			t.Errorf("filemask %q: %v, want no error", value, diags)
		case cleaned != "" && (len(diags) != 1 || !strings.Contains(diags[0], `"`+cleaned+`"`)):
			t.Errorf("filemask %q: %v, want one error naming %q", value, diags, cleaned)
		}
	}
}

func TestComponentImportResolvesTheSlugPath(t *testing.T) {
	c, _ := fakeWeblate(t, referenceTree(storedReference))
	r, s := componentResourceWith(c)
	resp := resource.ImportStateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, nil)}}
	r.(resource.ResourceWithImportState).ImportState(context.Background(),
		resource.ImportStateRequest{ID: "docs/guides/api/reference"}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("ImportState: %v", resp.Diagnostics)
	}
	if id := stateAttribute[types.Int64](t, resp.State, "weblate_id"); id.ValueInt64() != 12 {
		t.Errorf("weblate_id = %v, want 12", id)
	}
	if p := stateAttribute[types.String](t, resp.State, "project"); p.ValueString() != "docs" {
		t.Errorf("project = %v, want docs", p)
	}
}

func TestComponentDeleteWaitsUntilTheComponentIsGone(t *testing.T) {
	c, requests := fakeWeblate(t, func(r request) (int, string) {
		if r.method == http.MethodDelete {
			return http.StatusNoContent, ""
		}
		return http.StatusNotFound, notFound
	})
	r, s := componentResourceWith(c)
	state := tfsdk.State{Schema: s.Schema, Raw: projectValue(s, referenceState)}

	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}
	if len(*requests) != 2 || (*requests)[0].method != http.MethodDelete || (*requests)[0].path != referencePath ||
		(*requests)[1].method != http.MethodGet || (*requests)[1].path != referencePath {
		t.Errorf("requests = %v, want DELETE and GET of the Component", *requests)
	}
}

func TestProviderRegistersComponentResource(t *testing.T) {
	ctx := context.Background()
	for _, factory := range New("test")().Resources(ctx) {
		var resp resource.MetadataResponse
		factory().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "weblate"}, &resp)
		if resp.TypeName == "weblate_component" {
			return
		}
	}
	t.Error("the provider has no weblate_component resource")
}
