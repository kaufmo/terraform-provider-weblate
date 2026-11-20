package provider

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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

// withoutWriteOnly returns attributes with the write-only ones removed, as the framework plans them.
func withoutWriteOnly(s resource.SchemaResponse, attributes map[string]any) map[string]any {
	m := withAttributes(attributes, nil)
	for name, a := range s.Schema.Attributes {
		if a.IsWriteOnly() {
			delete(m, name)
		}
	}
	return m
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
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, withoutWriteOnly(s, config))},
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

func TestComponentReadFindsAComponentAfterAProjectRename(t *testing.T) {
	tree := referenceTree(storedReference)
	resp := readComponentResource(t, func(r request) (int, string) {
		switch r.path {
		case "/api/components/":
			return http.StatusOK, `{"next": null, "results": [` + storedReference + `]}`
		case "/api/components/old/guides%2Fapi%2Freference/", "/api/projects/old/components/":
			return http.StatusNotFound, notFound
		}
		return tree(r)
	}, map[string]any{"id": "old/guides/api/reference", "weblate_id": int64(12), "project": "old"})

	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "docs/guides/api/reference" {
		t.Errorf("id = %v, want docs/guides/api/reference", id)
	}
	if p := stateAttribute[types.String](t, resp.State, "project"); p.ValueString() != "docs" {
		t.Errorf("project = %v, want docs", p)
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
	for k, v := range withoutWriteOnly(s, config) {
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
		resource.ModifyPlanRequest{Plan: p, State: st, Config: tfsdk.Config{Schema: s.Schema, Raw: p.Raw}}, &resp)
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

var credentialsConfig = withAttributes(referenceConfig, map[string]any{
	"repo_username": "x-access-token", "repo_password_wo": "p@ss:/", "repo_password_wo_version": int64(1),
	"push": "http://git.example/push.git", "push_username": "bot", "push_password_wo": "secret"})

func TestComponentCreateSendsRepoAndPushWithCredentials(t *testing.T) {
	stored := strings.Replace(storedReference, `"push_branch": ""`,
		`"push_branch": "", "push": "http://git.example/push.git"`, 1)
	tree := referenceTree(stored)
	resp, requests := createComponent(t, func(r request) (int, string) {
		if r.method == http.MethodPost {
			return http.StatusCreated, stored
		}
		return tree(r)
	}, credentialsConfig)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}

	var body map[string]any
	for _, r := range requests {
		if r.method == http.MethodPost {
			body = r.body
		}
	}
	if body["repo"] != "http://x-access-token:p%40ss%3A%2F@git.example/demo.git" ||
		body["push"] != "http://bot:secret@git.example/push.git" {
		t.Errorf("repo = %v, push = %v, want the URLs with the credentials", body["repo"], body["push"])
	}
	for _, name := range []string{"repo_username", "repo_password_wo", "repo_password_wo_version",
		"push_username", "push_password_wo", "push_password_wo_version"} {
		if _, sent := body[name]; sent {
			t.Errorf("body has %s", name)
		}
	}
	if u := stateAttribute[types.String](t, resp.State, "repo_username"); u.ValueString() != "x-access-token" {
		t.Errorf("repo_username = %v, want it kept in state", u)
	}
	if v := stateAttribute[types.Int64](t, resp.State, "repo_password_wo_version"); v.ValueInt64() != 1 {
		t.Errorf("repo_password_wo_version = %v, want it kept in state", v)
	}
}

func TestComponentUpdateSendsRepoOnlyWhenItOrItsCredentialsChange(t *testing.T) {
	prior := withAttributes(referenceState, map[string]any{"repo_username": "x-access-token",
		"repo_password_wo_version": int64(1), "push": "http://git.example/push.git", "push_username": "bot"})
	for name, test := range map[string]struct {
		change   map[string]any
		wantRepo any
	}{
		"unrelated field": {map[string]any{"priority": int64(80)}, nil},
		"version bump": {map[string]any{"repo_password_wo_version": int64(2)},
			"http://x-access-token:p%40ss%3A%2F@git.example/demo.git"},
		"user name": {map[string]any{"repo_username": "oauth2"}, "http://oauth2:p%40ss%3A%2F@git.example/demo.git"},
		"url": {map[string]any{"repo": "http://git.example/other.git"},
			"http://x-access-token:p%40ss%3A%2F@git.example/other.git"},
		"credentials removed": {map[string]any{"repo_username": nil, "repo_password_wo": nil,
			"repo_password_wo_version": nil}, "http://git.example/demo.git"},
	} {
		t.Run(name, func(t *testing.T) {
			tree := referenceTree(storedReference)
			_, requests := updateComponent(t, func(r request) (int, string) {
				if r.method == http.MethodPatch {
					return http.StatusOK, storedReference
				}
				return tree(r)
			}, prior, withAttributes(credentialsConfig, test.change))
			p := patches(requests)
			if len(p) != 1 {
				t.Fatalf("requests = %v, want one PATCH", requests)
			}
			if p[0].body["repo"] != test.wantRepo {
				t.Errorf("repo = %v, want %v", p[0].body["repo"], test.wantRepo)
			}
			if _, sent := p[0].body["push"]; sent {
				t.Errorf("body = %v, want no push", p[0].body)
			}
			for _, name := range []string{"repo_username", "repo_password_wo", "repo_password_wo_version"} {
				if _, sent := p[0].body[name]; sent {
					t.Errorf("body has %s", name)
				}
			}
		})
	}
}

func TestComponentUpdateKeepsThePriorStateWhenThePatchFails(t *testing.T) {
	prior := withAttributes(referenceState, map[string]any{"repo_username": "x-access-token",
		"repo_password_wo_version": int64(1)})
	tree := referenceTree(storedReference)
	resp, _ := updateComponent(t, func(r request) (int, string) {
		if r.method == http.MethodPatch {
			return http.StatusBadRequest, `{"type": "validation_error", "errors": [{"code": "invalid",
				"detail": "Authentication failed.", "attr": "repo"}]}`
		}
		return tree(r)
	}, prior, withAttributes(credentialsConfig, map[string]any{"repo_password_wo_version": int64(2)}))

	if !resp.Diagnostics.HasError() {
		t.Fatal("Update succeeded, want the error of the PATCH")
	}
	// With the bumped version in state, a retry after fixing the password would not send repo again.
	if v := stateAttribute[types.Int64](t, resp.State, "repo_password_wo_version"); v.ValueInt64() != 1 {
		t.Errorf("repo_password_wo_version = %v, want the prior 1", v)
	}
}

func TestComponentReadRemovesCredentialsFromTheURLs(t *testing.T) {
	// An instance with HIDE_REPO_CREDENTIALS = False returns the URLs as stored.
	stored := strings.Replace(storedReference, `"repo": "http://git.example/demo.git"`,
		`"repo": "http://x-access-token:p%40ss@git.example/demo.git", "push": "ssh://git@git.example/demo.git"`, 1)
	resp := readComponentResource(t, referenceTree(stored), referenceState)
	if repo := stateAttribute[trimmedString](t, resp.State, "repo"); repo.ValueString() != "http://git.example/demo.git" {
		t.Errorf("repo = %v, want it without credentials", repo)
	}
	if push := stateAttribute[trimmedString](t, resp.State, "push"); push.ValueString() != "ssh://git.example/demo.git" {
		t.Errorf("push = %v, want it without the user name", push)
	}
}

func TestComponentConfigWithCredentialsInTheWrongPlaceFails(t *testing.T) {
	r, s := componentResourceWith(nil)
	for name, test := range map[string]struct {
		config map[string]any
		path   string
		detail string
	}{
		"password without user name": {withAttributes(credentialsConfig, map[string]any{"repo_username": nil}),
			"repo_password_wo", "repo_username"},
		"user info in repo": {withAttributes(referenceConfig, map[string]any{
			"repo": "https://user:token@git.example/demo.git"}), "repo", "repo_password_wo"},
		"user name in push": {withAttributes(referenceConfig, map[string]any{
			"push": "ssh://git@git.example/demo.git"}), "push", "push_password_wo"},
		"user name without push": {withAttributes(credentialsConfig, map[string]any{"push": nil}),
			"push_username", "push"},
		"user name with a URL without host": {withAttributes(credentialsConfig, map[string]any{
			"repo": "git@git.example:demo.git"}), "repo_username", "repo"},
		"valid": {credentialsConfig, "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			var resp resource.ValidateConfigResponse
			r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
				Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, test.config)}}, &resp)
			errs := resp.Diagnostics.Errors()
			if test.path == "" {
				if len(errs) != 0 {
					t.Errorf("diagnostics = %v, want none", errs)
				}
				return
			}
			if len(errs) != 1 || !strings.Contains(errs[0].Detail(), test.detail) {
				t.Fatalf("diagnostics = %v, want one error that names %s", errs, test.detail)
			}
			if d, ok := errs[0].(diag.DiagnosticWithPath); !ok || !d.Path().Equal(path.Root(test.path)) {
				t.Errorf("error is not at %s: %v", test.path, errs[0])
			}
		})
	}
}

var linkedConfig = withAttributes(referenceConfig, map[string]any{"slug": "linked", "name": "Linked",
	"repo": "weblate://docs/guides/api/reference"})

func TestComponentConfigOfALinkedComponentRejectsRepositorySettings(t *testing.T) {
	r, s := componentResourceWith(nil)
	for name, value := range map[string]any{"push": "http://git.example/push.git", "branch": "main",
		"push_branch": "", "vcs_params": `{}`, "push_on_commit": false, "push_on_update": true,
		"commit_pending_age": int64(1), "auto_lock_error": false, "repo_username": "bot",
		"repo_password_wo": "secret", "repo_password_wo_version": int64(1), "push_username": "bot",
		"push_password_wo": "secret", "push_password_wo_version": int64(1), "": nil} {
		t.Run(name, func(t *testing.T) {
			config := linkedConfig
			if name != "" {
				config = withAttributes(linkedConfig, map[string]any{name: value})
			}
			var resp resource.ValidateConfigResponse
			r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
				Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)}}, &resp)
			errs := resp.Diagnostics.Errors()
			if name == "" {
				if len(errs) != 0 {
					t.Errorf("diagnostics = %v, want none", errs)
				}
				return
			}
			if len(errs) != 1 || !strings.Contains(errs[0].Detail(), "weblate://docs/guides/api/reference") {
				t.Fatalf("diagnostics = %v, want one error that names the linked repo", errs)
			}
			if d, ok := errs[0].(diag.DiagnosticWithPath); !ok || !d.Path().Equal(path.Root(name)) {
				t.Errorf("error is not at %s: %v", name, errs[0])
			}
		})
	}
}

// storedLinked is the Component docs/linked, linked to docs/guides/api/reference. Weblate returns the repository
// settings of reference for it.
const storedLinked = `{"id": 13, "slug": "linked", "name": "Linked",
	"project": {"id": 7, "slug": "docs", "url": "http://weblate.example/api/projects/docs/"},
	"category": null, "source_language": {"code": "en", "name": "English"},
	"vcs": "git", "vcs_params": {}, "repo": "http://git.example/demo.git", "branch": "main", "push_branch": "",
	"push": "", "filemask": "po/*.po", "file_format": "po", "git_export": "",
	"linked_component": "http://weblate.example/api/components/docs/guides%252Fapi%252Freference/",
	"url": "http://weblate.example/api/components/docs/linked/", "task_url": null}`

func TestComponentReadOfALinkedComponentKeepsTheLinkInRepo(t *testing.T) {
	for name, stateRepo := range map[string]any{"refresh": "weblate://docs/guides/api/reference",
		"import": nil, "linked outside OpenTofu": "http://git.example/demo.git"} {
		t.Run(name, func(t *testing.T) {
			resp := readComponentResource(t, func(r request) (int, string) {
				if r.path == "/api/components/docs/linked/" {
					return http.StatusOK, storedLinked
				}
				return http.StatusNotFound, notFound
			}, map[string]any{"id": "docs/linked", "weblate_id": int64(13), "project": "docs", "repo": stateRepo})

			if repo := stateAttribute[trimmedString](t, resp.State, "repo"); repo.ValueString() != "weblate://docs/guides/api/reference" {
				t.Errorf("repo = %v, want weblate://docs/guides/api/reference", repo)
			}
			if l := stateAttribute[types.String](t, resp.State, "linked_component"); l.ValueString() != "docs/guides/api/reference" {
				t.Errorf("linked_component = %v, want docs/guides/api/reference", l)
			}
			if b := stateAttribute[trimmedString](t, resp.State, "branch"); b.ValueString() != "main" {
				t.Errorf("branch = %v, want the branch of the linked Component", b)
			}
		})
	}
}

func TestComponentCreateOfALinkedComponentPassesTheReadBack(t *testing.T) {
	resp, _ := createComponent(t, func(r request) (int, string) {
		if r.method == http.MethodPost || r.path == "/api/components/docs/linked/" {
			return http.StatusCreated, storedLinked
		}
		return http.StatusNotFound, notFound
	}, withAttributes(linkedConfig, map[string]any{"category": nil, "file_format_params": nil}))
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
	if repo := stateAttribute[trimmedString](t, resp.State, "repo"); repo.ValueString() != "weblate://docs/guides/api/reference" {
		t.Errorf("repo = %v, want the configured link", repo)
	}
}

// storedWithoutVCSView is storedReference as Weblate returns it to a token without the permission vcs.view.
const storedWithoutVCSView = `{"id": 12, "slug": "reference", "name": "Reference",
	"project": {"id": 7, "slug": "docs", "url": "http://weblate.example/api/projects/docs/"},
	"category": "http://weblate.example/api/categories/9/", "source_language": {"code": "en", "name": "English"},
	"vcs": null, "vcs_params": null, "repo": null, "branch": null, "push_branch": null, "push": null,
	"filemask": null, "screenshot_filemask": null, "git_export": null, "repoweb": null, "repoweb_translations": null,
	"linked_component": null, "file_format": "po", "file_format_params": {"po_line_wrap": 77}, "priority": 100,
	"url": "http://weblate.example/api/components/docs/guides%252Fapi%252Freference/", "task_url": null}`

func TestComponentReadWithoutVCSViewKeepsTheHiddenFields(t *testing.T) {
	state := withAttributes(referenceState, map[string]any{"vcs": "git", "vcs_params": `{}`, "git_export": "",
		"linked_component": "docs/other"})
	resp := readComponentResource(t, referenceTree(storedWithoutVCSView), state)

	for name, want := range map[string]string{"repo": "http://git.example/demo.git", "branch": "main",
		"filemask": "po/*.po", "vcs": "git", "vcs_params": "{}", "git_export": "", "linked_component": "docs/other"} {
		var got attr.Value
		resp.Diagnostics.Append(resp.State.GetAttribute(context.Background(), path.Root(name), &got)...)
		if got.String() != strconv.Quote(want) {
			t.Errorf("%s = %v, want %q from state", name, got, want)
		}
	}
	if push := stateAttribute[trimmedString](t, resp.State, "push"); !push.IsNull() {
		t.Errorf("push = %v, want null as in state", push)
	}
	if p := stateAttribute[types.Int64](t, resp.State, "priority"); p.ValueInt64() != 100 {
		t.Errorf("priority = %v, want 100 from Weblate", p)
	}
	warnings := resp.Diagnostics.Warnings()
	if len(warnings) != 1 || !strings.Contains(warnings[0].Detail(), "vcs.view") {
		t.Errorf("diagnostics = %v, want one warning that names vcs.view", resp.Diagnostics)
	}
}

func TestComponentCreateWithoutVCSViewPassesTheReadBack(t *testing.T) {
	tree := referenceTree(storedWithoutVCSView)
	c, _ := fakeWeblate(t, func(r request) (int, string) {
		if r.method == http.MethodPost {
			return http.StatusCreated, storedWithoutVCSView
		}
		return tree(r)
	})
	r, s := componentResourceWith(c)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s.Schema,
		Raw: tftypes.NewValue(s.Schema.Type().TerraformType(context.Background()), nil)}}
	r.Create(context.Background(), resource.CreateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, referenceConfig)},
		Plan: tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, withAttributes(referenceConfig,
			map[string]any{"branch": tftypes.UnknownValue, "git_export": tftypes.UnknownValue}))},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
	if repo := stateAttribute[trimmedString](t, resp.State, "repo"); repo.ValueString() != "http://git.example/demo.git" {
		t.Errorf("repo = %v, want the planned value", repo)
	}
	// Planned as unknown, which state cannot hold after an apply.
	if b := stateAttribute[trimmedString](t, resp.State, "branch"); !b.IsNull() {
		t.Errorf("branch = %v, want null", b)
	}
}

func TestComponentReadOfAnUnlinkedComponentHasNoLinkedComponent(t *testing.T) {
	resp := readComponentResource(t, referenceTree(storedReference), referenceState)
	if l := stateAttribute[types.String](t, resp.State, "linked_component"); !l.IsNull() {
		t.Errorf("linked_component = %v, want null", l)
	}
}
