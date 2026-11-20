package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

const storedScopedGroup = `{"id": 12, "name": "Reviewers", "defining_project": "https://weblate.example/api/projects/docs/",
	"defining_workspace": null, "project_selection": 0, "language_selection": 1, "enforced_2fa": false,
	"roles": [], "languages": [], "projects": ["https://weblate.example/api/projects/docs/"]}`

const storedGroup = `{"id": 12, "name": "Reviewers", "defining_project": null, "project_selection": 3,
	"language_selection": 1, "enforced_2fa": true}`

func groupResourceWith(c *client.Client) (resource.ResourceWithConfigure, resource.SchemaResponse) {
	ctx := context.Background()
	r := NewGroupResource().(resource.ResourceWithConfigure)
	r.Configure(ctx, resource.ConfigureRequest{ProviderData: c}, &resource.ConfigureResponse{})
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	return r, schemaResp
}

func createGroup(t *testing.T, stored string, config map[string]any) (resource.CreateResponse, []request) {
	t.Helper()
	c, requests := fakeWeblate(t, func(request) (int, string) { return http.StatusCreated, stored })
	r, s := groupResourceWith(c)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, nil)}}
	r.Create(context.Background(), resource.CreateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, config)},
	}, &resp)
	return resp, *requests
}

func TestGroupCreateSendsTheProjectAsURLAndSelectionsAsNumbers(t *testing.T) {
	resp, requests := createGroup(t, storedScopedGroup, map[string]any{"name": "Reviewers", "project": "docs",
		"language_selection": "all"})
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}

	if len(requests) != 1 || requests[0].method != http.MethodPost || requests[0].path != "/api/groups/" {
		t.Fatalf("requests = %v, want one POST /api/groups/", requests)
	}
	body := requests[0].body
	if len(body) != 3 || body["name"] != "Reviewers" || body["language_selection"] != float64(1) ||
		!strings.HasSuffix(body["defining_project"].(string), "/api/projects/docs/") {
		t.Errorf("body = %v, want name, language_selection 1 and the API URL of Project docs", body)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "12" {
		t.Errorf("id = %v, want 12", id)
	}
	if p := stateAttribute[types.String](t, resp.State, "project"); p.ValueString() != "docs" {
		t.Errorf("project = %v, want docs", p)
	}
	if v := stateAttribute[types.String](t, resp.State, "project_selection"); v.ValueString() != "manual" {
		t.Errorf("project_selection = %v, want manual", v)
	}
}

func TestGroupCreateStoresAnInstanceWideGroup(t *testing.T) {
	resp, _ := createGroup(t, storedGroup, map[string]any{"name": "Reviewers", "project_selection": "all_public",
		"enforced_2fa": true})
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
	if p := stateAttribute[types.String](t, resp.State, "project"); !p.IsNull() {
		t.Errorf("project = %v, want null", p)
	}
	if v := stateAttribute[types.String](t, resp.State, "project_selection"); v.ValueString() != "all_public" {
		t.Errorf("project_selection = %v, want all_public", v)
	}
}

func TestGroupCreateFailsWhenWeblateStoresAnotherSelection(t *testing.T) {
	// Weblate sets project_selection to manual on a Group that it creates for a Project.
	resp, _ := createGroup(t, storedScopedGroup, map[string]any{"name": "Reviewers", "project": "docs",
		"project_selection": "all"})
	errs := resp.Diagnostics.Errors()
	if len(errs) != 1 || !strings.Contains(errs[0].Detail(), "project_selection") {
		t.Fatalf("diagnostics = %v, want one error on project_selection", resp.Diagnostics)
	}
}

func readGroupResource(t *testing.T, respond func(r request) (int, string), state map[string]any) resource.ReadResponse {
	t.Helper()
	c, _ := fakeWeblate(t, respond)
	r, s := groupResourceWith(c)
	resp := resource.ReadResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, state)}}
	r.Read(context.Background(), resource.ReadRequest{State: resp.State}, &resp)
	return resp
}

func TestGroupReadAfterImportUsesTheNumericID(t *testing.T) {
	resp := readGroupResource(t, func(r request) (int, string) {
		if r.path != "/api/groups/12/" {
			t.Errorf("path = %s", r.path)
		}
		return http.StatusOK, storedGroup
	}, map[string]any{"id": "12"})
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}

	if name := stateAttribute[trimmedString](t, resp.State, "name"); name.ValueString() != "Reviewers" {
		t.Errorf("name = %v", name)
	}
	if v := stateAttribute[types.String](t, resp.State, "language_selection"); v.ValueString() != "all" {
		t.Errorf("language_selection = %v, want all", v)
	}
	if v := stateAttribute[types.Bool](t, resp.State, "enforced_2fa"); !v.ValueBool() {
		t.Errorf("enforced_2fa = %v, want true", v)
	}
}

func TestGroupReadRemovesADeletedGroup(t *testing.T) {
	resp := readGroupResource(t, func(request) (int, string) {
		return http.StatusNotFound, `{"detail": "No Group matches the given query."}`
	}, map[string]any{"id": "12", "name": "Reviewers"})
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Errorf("state = %v, want the resource removed", resp.State.Raw)
	}
}

func TestGroupReadFailsOnAnUnknownSelection(t *testing.T) {
	resp := readGroupResource(t, func(request) (int, string) {
		return http.StatusOK, strings.Replace(storedGroup, `"project_selection": 3`, `"project_selection": 9`, 1)
	}, map[string]any{"id": "12"})
	if errs := resp.Diagnostics.Errors(); len(errs) != 1 || !strings.Contains(errs[0].Detail(), "9") {
		t.Errorf("diagnostics = %v, want one error naming the value 9", resp.Diagnostics)
	}
}

func updateGroup(t *testing.T, respond func(r request) (int, string), prior, config, plan map[string]any) (resource.UpdateResponse, []request) {
	t.Helper()
	c, requests := fakeWeblate(t, respond)
	r, s := groupResourceWith(c)
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)}}
	r.Update(context.Background(), resource.UpdateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, plan)},
		State:  tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)},
	}, &resp)
	return resp, *requests
}

var priorScopedGroup = map[string]any{"id": "12", "name": "Reviewers", "project": "docs", "project_selection": "manual",
	"language_selection": "all", "enforced_2fa": false}

func withAttributes(base map[string]any, changes map[string]any) map[string]any {
	m := map[string]any{}
	for k, v := range base {
		m[k] = v
	}
	for k, v := range changes {
		m[k] = v
	}
	return m
}

func TestGroupUpdatePatchesOnlyChangedAttributes(t *testing.T) {
	resp, requests := updateGroup(t, func(request) (int, string) {
		return http.StatusOK, strings.Replace(storedScopedGroup, `"language_selection": 1`, `"language_selection": 0`, 1)
	}, priorScopedGroup,
		map[string]any{"name": "Reviewers", "project": "docs", "language_selection": "manual"},
		withAttributes(priorScopedGroup, map[string]any{"language_selection": "manual"}))
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	if len(requests) != 1 || requests[0].method != http.MethodPatch || requests[0].path != "/api/groups/12/" {
		t.Fatalf("requests = %v, want one PATCH /api/groups/12/", requests)
	}
	if got := requests[0].body; len(got) != 1 || got["language_selection"] != float64(0) {
		t.Errorf("body = %v, want only language_selection 0", got)
	}
}

func TestGroupUpdateFailsOnAnotherProjectWithoutWriting(t *testing.T) {
	resp, requests := updateGroup(t, func(request) (int, string) { return http.StatusOK, storedScopedGroup },
		priorScopedGroup,
		map[string]any{"name": "Translators", "project": "guides"},
		withAttributes(priorScopedGroup, map[string]any{"name": "Translators", "project": "guides"}))

	errs := resp.Diagnostics.Errors()
	if len(errs) != 1 || !strings.Contains(errs[0].Detail(), "-replace") {
		t.Fatalf("diagnostics = %v, want one error that points to -replace", resp.Diagnostics)
	}
	if want := path.Root("project"); !errs[0].(diag.DiagnosticWithPath).Path().Equal(want) {
		t.Errorf("error path = %v, want %v", errs[0], want)
	}
	for _, r := range requests {
		if r.method != http.MethodGet {
			t.Errorf("request %s %s, want no write", r.method, r.path)
		}
	}
}

func TestGroupUpdateFollowsARenamedProject(t *testing.T) {
	renamed := strings.ReplaceAll(storedScopedGroup, "/projects/docs/", "/projects/guides/")
	resp, requests := updateGroup(t, func(request) (int, string) { return http.StatusOK, renamed },
		priorScopedGroup,
		map[string]any{"name": "Reviewers", "project": "guides"},
		withAttributes(priorScopedGroup, map[string]any{"project": "guides"}))
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	for _, r := range requests {
		if r.method != http.MethodGet {
			t.Errorf("request %s %s, want only reads", r.method, r.path)
		}
	}
	if p := stateAttribute[types.String](t, resp.State, "project"); p.ValueString() != "guides" {
		t.Errorf("project = %v, want guides", p)
	}
}

func TestGroupDeleteIgnoresAGroupThatIsGone(t *testing.T) {
	c, requests := fakeWeblate(t, func(request) (int, string) {
		return http.StatusNotFound, `{"detail": "No Group matches the given query."}`
	})
	r, s := groupResourceWith(c)
	state := tfsdk.State{Schema: s.Schema, Raw: projectValue(s, map[string]any{"id": "12", "name": "Reviewers"})}
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}
	if len(*requests) != 1 || (*requests)[0].method != http.MethodDelete || (*requests)[0].path != "/api/groups/12/" {
		t.Errorf("requests = %v, want one DELETE /api/groups/12/", *requests)
	}
}

func TestGroupImportRejectsANonNumericID(t *testing.T) {
	r, s := groupResourceWith(nil)
	resp := resource.ImportStateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, nil)}}
	r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: "reviewers"}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Error("ImportState accepted a non-numeric ID")
	}
}

func TestGroupSchemaRejectsAnUnknownSelection(t *testing.T) {
	_, s := groupResourceWith(nil)
	for attribute, value := range map[string]string{"project_selection": "everything", "language_selection": "all_public"} {
		var resp validator.StringResponse
		for _, v := range s.Schema.Attributes[attribute].(interface{ StringValidators() []validator.String }).StringValidators() {
			v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root(attribute),
				ConfigValue: types.StringValue(value)}, &resp)
		}
		if !resp.Diagnostics.HasError() {
			t.Errorf("%s = %q passed validation", attribute, value)
		}
	}
}

func TestProviderRegistersGroupResource(t *testing.T) {
	ctx := context.Background()
	for _, factory := range New("test")().Resources(ctx) {
		var resp resource.MetadataResponse
		factory().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "weblate"}, &resp)
		if resp.TypeName == "weblate_group" {
			return
		}
	}
	t.Error("the provider has no weblate_group resource")
}
