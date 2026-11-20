package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// stringSet returns the raw value of a set of strings for projectValue.
func stringSet(values ...string) []tftypes.Value {
	set := []tftypes.Value{}
	for _, v := range values {
		set = append(set, tftypes.NewValue(tftypes.String, v))
	}
	return set
}

func validateGroupConfig(t *testing.T, config map[string]any) diag.Diagnostics {
	t.Helper()
	r, s := groupResourceWith(nil)
	var resp resource.ValidateConfigResponse
	r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
	}, &resp)
	return resp.Diagnostics
}

func TestGroupValidateConfigRejectsGrantsThatTheSelectionIgnores(t *testing.T) {
	for name, tc := range map[string]struct {
		config    map[string]any
		attribute string
		mention   string
	}{
		"projects without manual": {map[string]any{"name": "G", "project_selection": "all",
			"projects": stringSet("docs")}, "projects", `"manual"`},
		"component_lists without component_list": {map[string]any{"name": "G", "project_selection": "manual",
			"component_lists": stringSet("core")}, "component_lists", `"component_list"`},
		"languages with all": {map[string]any{"name": "G", "language_selection": "all",
			"languages": stringSet("de")}, "languages", `"manual"`},
		"projects on a scoped Group": {map[string]any{"name": "G", "project": "docs",
			"projects": stringSet("docs")}, "projects", "project"},
		"component_lists on a scoped Group": {map[string]any{"name": "G", "project": "docs",
			"component_lists": stringSet("core")}, "component_lists", "project"},
		"another selection on a scoped Group": {map[string]any{"name": "G", "project": "docs",
			"project_selection": "all"}, "project_selection", "project"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := validateGroupConfig(t, tc.config).Errors()
			if len(errs) != 1 {
				t.Fatalf("diagnostics = %v, want one error", errs)
			}
			if got := errs[0].(diag.DiagnosticWithPath).Path(); !got.Equal(path.Root(tc.attribute)) {
				t.Errorf("error path = %v, want %s", got, tc.attribute)
			}
			if !strings.Contains(errs[0].Detail(), tc.mention) {
				t.Errorf("detail = %q, want it to mention %s", errs[0].Detail(), tc.mention)
			}
		})
	}
}

const groupWithGrants = `{"id": 12, "name": "Translators", "defining_project": null, "project_selection": 0,
	"language_selection": 0, "enforced_2fa": false,
	"roles": ["https://weblate.example/api/roles/3/"],
	"languages": ["https://weblate.example/api/languages/de/", "https://weblate.example/api/languages/sr@latin/"],
	"projects": ["https://weblate.example/api/projects/docs/"],
	"componentlists": [],
	"components": ["https://weblate.example/api/components/docs/guides%252Fapi/"]}`

const storedRoles = `{"count": 2, "next": null, "results": [{"id": 2, "name": "Administration", "permissions": []},
	{"id": 3, "name": "Review strings", "permissions": []}]}`

// grantsWeblate answers like a Weblate instance with the Roles of storedRoles, a Project docs with ID 7, a Component
// docs/guides/api with ID 9 and a Component list core with ID 2. It answers the create or PATCH of the Group with
// written and a GET of the Group with stored.
func grantsWeblate(written, stored string) func(r request) (int, string) {
	return func(r request) (int, string) {
		switch {
		case r.method == http.MethodGet && r.path == "/api/groups/12/":
			return http.StatusOK, stored
		case r.method == http.MethodPost && r.path == "/api/groups/":
			return http.StatusCreated, written
		case r.method == http.MethodPatch && r.path == "/api/groups/12/":
			return http.StatusOK, written
		case r.path == "/api/roles/":
			return http.StatusOK, storedRoles
		case r.path == "/api/projects/docs/":
			return http.StatusOK, `{"id": 7, "slug": "docs"}`
		case r.path == "/api/components/docs/guides%2Fapi/":
			return http.StatusOK, `{"id": 9, "slug": "api"}`
		case r.path == "/api/component-lists/core/":
			return http.StatusOK, `{"id": 2, "slug": "core"}`
		case r.method == http.MethodPost && strings.HasPrefix(r.path, "/api/groups/12/"):
			return http.StatusOK, stored
		case r.method == http.MethodDelete && strings.HasPrefix(r.path, "/api/groups/12/"):
			return http.StatusNoContent, ""
		}
		return http.StatusNotFound, `{"detail": "Not found."}`
	}
}

func setAttribute(t *testing.T, state tfsdk.State, name string) []string {
	t.Helper()
	var values []string
	if diags := state.GetAttribute(context.Background(), path.Root(name), &values); diags.HasError() {
		t.Fatalf("reading %s from state: %v", name, diags)
	}
	slices.Sort(values)
	return values
}

func TestGroupReadWritesTheGrantsByName(t *testing.T) {
	resp := readGroupResource(t, grantsWeblate("", groupWithGrants), map[string]any{"id": "12"})
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	for attribute, want := range map[string][]string{
		"roles":           {"Review strings"},
		"languages":       {"de", "sr@latin"},
		"projects":        {"docs"},
		"component_lists": {},
		"components":      {"docs/guides/api"},
	} {
		if got := setAttribute(t, resp.State, attribute); !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", attribute, got, want)
		}
	}
}

const groupWithAHiddenRole = `{"id": 12, "name": "Translators", "defining_project": null, "project_selection": 0,
	"language_selection": 0, "enforced_2fa": false,
	"roles": ["https://weblate.example/api/roles/3/", "https://weblate.example/api/roles/5/"]}`

func TestGroupReadSkipsARoleThatTheTokenCannotSeeWhenRolesAreOmitted(t *testing.T) {
	resp := readGroupResource(t, grantsWeblate("", groupWithAHiddenRole), map[string]any{"id": "12"})
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	if warnings := resp.Diagnostics.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0].Detail(), "role.view") {
		t.Errorf("diagnostics = %v, want one warning that names role.view", resp.Diagnostics)
	}
	if roles := setAttribute(t, resp.State, "roles"); !slices.Equal(roles, []string{"Review strings"}) {
		t.Errorf("roles = %v, want only the visible Role", roles)
	}
}

func TestGroupReadFailsOnARoleThatTheTokenCannotSeeWhenRolesAreSet(t *testing.T) {
	c, _ := fakeWeblate(t, grantsWeblate("", groupWithAHiddenRole))
	r, s := groupResourceWith(c)
	ctx := context.Background()
	resp := resource.ReadResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, map[string]any{"id": "12"})}}
	newPrivate(&resp.Private)
	// Create and update record a configuration that sets roles.
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, "roles_configured", []byte("true"))...)
	r.Read(ctx, resource.ReadRequest{State: resp.State, Private: resp.Private}, &resp)

	if errs := resp.Diagnostics.Errors(); len(errs) != 1 || !strings.Contains(errs[0].Detail(), "role.view") {
		t.Errorf("diagnostics = %v, want one error that names role.view", resp.Diagnostics)
	}
}

func TestGroupCreateRecordsWhetherRolesAreConfigured(t *testing.T) {
	for name, tc := range map[string]struct {
		config map[string]any
		want   string
	}{
		"roles set":     {map[string]any{"name": "Translators", "roles": stringSet()}, "true"},
		"roles omitted": {map[string]any{"name": "Translators"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			resp, _ := createGroup(t, groupWithoutGrants, tc.config)
			if resp.Diagnostics.HasError() {
				t.Fatalf("Create: %v", resp.Diagnostics)
			}
			if got, _ := resp.Private.GetKey(context.Background(), "roles_configured"); string(got) != tc.want {
				t.Errorf("roles_configured = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGroupValidateConfigAcceptsMatchingGrants(t *testing.T) {
	for name, config := range map[string]map[string]any{
		"projects with project_selection omitted": {"name": "G", "projects": stringSet("docs")},
		"projects with manual":                    {"name": "G", "project_selection": "manual", "projects": stringSet()},
		"component_lists with project_selection omitted": {"name": "G", "component_lists": stringSet("core")},
		"component_lists with component_list": {"name": "G", "project_selection": "component_list",
			"component_lists": stringSet("core")},
		"languages with manual": {"name": "G", "language_selection": "manual", "languages": stringSet("de")},
		"scoped Group with its own Grants": {"name": "G", "project": "docs", "project_selection": "manual",
			"roles": stringSet("Review"), "languages": stringSet("de"), "components": stringSet("docs/api")},
		"unknown selection": {"name": "G", "project_selection": tftypes.UnknownValue,
			"projects": stringSet("docs")},
	} {
		t.Run(name, func(t *testing.T) {
			if diags := validateGroupConfig(t, config); diags.HasError() {
				t.Errorf("diagnostics = %v, want none", diags)
			}
		})
	}
}

const groupWithoutGrants = `{"id": 12, "name": "Translators", "defining_project": null, "project_selection": 0,
	"language_selection": 0, "enforced_2fa": false, "roles": [], "languages": [], "projects": [],
	"componentlists": [], "components": []}`

// writes returns the requests that change something, as "METHOD path body".
func writes(requests []request) []string {
	var got []string
	for _, r := range requests {
		if r.method == http.MethodGet {
			continue
		}
		line := r.method + " " + r.path
		if r.body != nil {
			b, _ := json.Marshal(r.body)
			line += " " + string(b)
		}
		got = append(got, line)
	}
	return got
}

func TestGroupCreateAddsTheConfiguredGrants(t *testing.T) {
	c, requests := fakeWeblate(t, grantsWeblate(groupWithoutGrants, groupWithGrants))
	r, s := groupResourceWith(c)
	config := map[string]any{"name": "Translators", "roles": stringSet("Review strings"),
		"languages": stringSet("de", "sr@latin"), "projects": stringSet("docs"),
		"components": stringSet("docs/guides/api")}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, nil)}}
	newPrivate(&resp.Private)
	r.Create(context.Background(), resource.CreateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, config)},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}

	got := writes(*requests)
	want := []string{
		`POST /api/groups/ {"name":"Translators"}`,
		`POST /api/groups/12/roles/ {"role_id":3}`,
		`POST /api/groups/12/projects/ {"project_id":7}`,
		`POST /api/groups/12/components/ {"component_id":9}`,
	}
	// The Languages come from a set, so their order is not fixed.
	languages := got[2:4]
	slices.Sort(languages)
	if !slices.Equal(languages, []string{`POST /api/groups/12/languages/ {"language_code":"de"}`,
		`POST /api/groups/12/languages/ {"language_code":"sr@latin"}`}) {
		t.Errorf("language requests = %v", languages)
	}
	if got = slices.Delete(got, 2, 4); !slices.Equal(got, want) {
		t.Errorf("requests = %q, want %q", got, want)
	}
	if roles := setAttribute(t, resp.State, "roles"); !slices.Equal(roles, []string{"Review strings"}) {
		t.Errorf("roles = %v", roles)
	}
}

func TestGroupCreateFailsOnAnUnknownRoleAndKeepsTheGroup(t *testing.T) {
	c, requests := fakeWeblate(t, grantsWeblate(groupWithoutGrants, groupWithoutGrants))
	r, s := groupResourceWith(c)
	config := map[string]any{"name": "Translators", "roles": stringSet("Reviewer")}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, nil)}}
	newPrivate(&resp.Private)
	r.Create(context.Background(), resource.CreateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, config)},
	}, &resp)

	errs := resp.Diagnostics.Errors()
	if len(errs) != 1 || !strings.Contains(errs[0].Detail(), `"Reviewer"`) {
		t.Fatalf("diagnostics = %v, want one error that names the Role", resp.Diagnostics)
	}
	if got := errs[0].(diag.DiagnosticWithPath).Path(); !got.Equal(path.Root("roles")) {
		t.Errorf("error path = %v, want roles", got)
	}
	if got := writes(*requests); len(got) != 1 {
		t.Errorf("requests = %v, want only the create", got)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "12" {
		t.Errorf("id = %v, want the created Group in state", id)
	}
}

func TestGroupUpdateRemovesGrantsAddedOutsideTheConfiguration(t *testing.T) {
	stored := strings.Replace(groupWithGrants, `"roles": ["https://weblate.example/api/roles/3/"]`,
		`"roles": ["https://weblate.example/api/roles/3/", "https://weblate.example/api/roles/2/"]`, 1)
	c, requests := fakeWeblate(t, grantsWeblate("", stored))
	r, s := groupResourceWith(c)
	prior := map[string]any{"id": "12", "name": "Translators", "roles": stringSet("Review strings", "Administration")}
	config := map[string]any{"name": "Translators", "roles": stringSet("Review strings")}
	plan := map[string]any{"id": "12", "name": "Translators", "roles": stringSet("Review strings")}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)}}
	newPrivate(&resp.Private)
	r.Update(context.Background(), resource.UpdateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, plan)},
		State:  tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)},
	}, &resp)

	if got := writes(*requests); !slices.Equal(got, []string{"DELETE /api/groups/12/roles/2/"}) {
		t.Errorf("requests = %q, want only the DELETE of Role 2", got)
	}
	// The fake still returns Role 2 after the DELETE, so the read-back check reports it.
	if errs := resp.Diagnostics.Errors(); len(errs) != 1 || !strings.Contains(errs[0].Detail(), "Administration") {
		t.Errorf("diagnostics = %v, want one read-back error that names Administration", resp.Diagnostics)
	}
}

func TestGroupUpdatePatchesTheSelectionBeforeTheGrants(t *testing.T) {
	c, requests := fakeWeblate(t, grantsWeblate(groupWithoutGrants, strings.Replace(groupWithoutGrants,
		`"projects": []`, `"projects": ["https://weblate.example/api/projects/docs/"]`, 1)))
	r, s := groupResourceWith(c)
	prior := map[string]any{"id": "12", "name": "Translators", "project_selection": "all", "projects": stringSet()}
	config := map[string]any{"name": "Translators", "project_selection": "manual", "projects": stringSet("docs")}
	plan := withAttributes(config, map[string]any{"id": "12"})
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)}}
	newPrivate(&resp.Private)
	r.Update(context.Background(), resource.UpdateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, plan)},
		State:  tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}

	want := []string{`PATCH /api/groups/12/ {"project_selection":0}`, `POST /api/groups/12/projects/ {"project_id":7}`}
	if got := writes(*requests); !slices.Equal(got, want) {
		t.Errorf("requests = %q, want %q", got, want)
	}
}

func TestGroupUpdateLeavesOmittedGrantsAlone(t *testing.T) {
	c, requests := fakeWeblate(t, grantsWeblate(groupWithGrants, groupWithGrants))
	r, s := groupResourceWith(c)
	prior := map[string]any{"id": "12", "name": "Reviewers", "roles": stringSet("Review strings")}
	config := map[string]any{"name": "Translators"}
	plan := withAttributes(prior, map[string]any{"name": "Translators"})
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)}}
	newPrivate(&resp.Private)
	r.Update(context.Background(), resource.UpdateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, plan)},
		State:  tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	if got := writes(*requests); !slices.Equal(got, []string{`PATCH /api/groups/12/ {"name":"Translators"}`}) {
		t.Errorf("requests = %q, want only the PATCH of name", got)
	}
	if langs := setAttribute(t, resp.State, "languages"); !slices.Equal(langs, []string{"de", "sr@latin"}) {
		t.Errorf("languages = %v, want the Languages that Weblate stores", langs)
	}
}

func TestGroupUpdateStoresTheGroupAfterAFailedGrant(t *testing.T) {
	stored := strings.Replace(groupWithoutGrants, `"roles": []`, `"roles": ["https://weblate.example/api/roles/3/"]`, 1)
	c, _ := fakeWeblate(t, func(r request) (int, string) {
		if r.path == "/api/groups/12/projects/" {
			return http.StatusBadRequest, `{"type": "validation_error", "errors": [{"code": "invalid",
				"detail": "Project not found.", "attr": "project_id"}]}`
		}
		return grantsWeblate("", stored)(r)
	})
	r, s := groupResourceWith(c)
	prior := map[string]any{"id": "12", "name": "Translators", "roles": stringSet(), "projects": stringSet()}
	config := map[string]any{"name": "Translators", "roles": stringSet("Review strings"), "projects": stringSet("docs")}
	plan := withAttributes(config, map[string]any{"id": "12"})
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)}}
	newPrivate(&resp.Private)
	r.Update(context.Background(), resource.UpdateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, plan)},
		State:  tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)},
	}, &resp)

	errs := resp.Diagnostics.Errors()
	if len(errs) != 1 || !strings.Contains(errs[0].Detail(), "Project not found.") {
		t.Fatalf("diagnostics = %v, want one error with Weblate's message", resp.Diagnostics)
	}
	if roles := setAttribute(t, resp.State, "roles"); !slices.Equal(roles, []string{"Review strings"}) {
		t.Errorf("roles = %v, want the Role that was added before the failure", roles)
	}
	if projects := setAttribute(t, resp.State, "projects"); len(projects) != 0 {
		t.Errorf("projects = %v, want none", projects)
	}
}

func planGroup(t *testing.T, prior, config map[string]any) tfsdk.Plan {
	t.Helper()
	resp := modifyGroupPlan(prior, config)
	if resp.Diagnostics.HasError() {
		t.Fatalf("ModifyPlan: %v", resp.Diagnostics)
	}
	return resp.Plan
}

func modifyGroupPlan(prior, config map[string]any) resource.ModifyPlanResponse {
	r, s := groupResourceWith(nil)
	// The plan as the framework hands it over: UseStateForUnknown has copied the prior Grants and project.
	plan := withAttributes(prior, config)
	resp := resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, plan)}}
	r.(resource.ResourceWithModifyPlan).ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   resp.Plan,
		State:  tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)},
	}, &resp)
	return resp
}

func TestGroupPlanLeavesOmittedProjectsUnknownWhenTheSelectionChanges(t *testing.T) {
	prior := map[string]any{"id": "12", "name": "G", "project_selection": "manual", "projects": stringSet("docs"),
		"component_lists": stringSet(), "roles": stringSet("Review strings")}
	plan := planGroup(t, prior, map[string]any{"name": "G", "project_selection": "all"})

	for attribute, wantUnknown := range map[string]bool{"projects": true, "component_lists": true, "roles": false} {
		var v types.Set
		plan.GetAttribute(context.Background(), path.Root(attribute), &v)
		if v.IsUnknown() != wantUnknown {
			t.Errorf("%s = %v, want unknown %v", attribute, v, wantUnknown)
		}
	}
}

func TestGroupPlanKeepsOmittedProjectsWithTheSameSelection(t *testing.T) {
	prior := map[string]any{"id": "12", "name": "G", "project_selection": "manual", "projects": stringSet("docs")}
	plan := planGroup(t, prior, map[string]any{"name": "H"})

	var v types.Set
	plan.GetAttribute(context.Background(), path.Root("projects"), &v)
	if v.IsUnknown() || len(v.Elements()) != 1 {
		t.Errorf("projects = %v, want the prior [docs]", v)
	}
}
