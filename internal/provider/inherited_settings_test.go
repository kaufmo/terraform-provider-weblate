package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// resourcesWithInheritedSettings returns weblate_project, weblate_category and weblate_component with a config that
// sets their required attributes.
func resourcesWithInheritedSettings() map[string]struct {
	resource resource.Resource
	config   map[string]any
} {
	return map[string]struct {
		resource resource.Resource
		config   map[string]any
	}{
		"weblate_project":   {NewProjectResource(), map[string]any{"name": "Docs", "slug": "docs"}},
		"weblate_category":  {NewCategoryResource(), map[string]any{"project": "docs", "name": "Guides", "slug": "guides"}},
		"weblate_component": {NewComponentResource(), referenceConfig},
	}
}

func TestInheritedSettingWithInheritTrueFailsValidation(t *testing.T) {
	for name, test := range resourcesWithInheritedSettings() {
		var s resource.SchemaResponse
		test.resource.Schema(context.Background(), resource.SchemaRequest{}, &s)
		validate := func(settings map[string]any) diag.Diagnostics {
			var resp resource.ValidateConfigResponse
			test.resource.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(),
				resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: s.Schema,
					Raw: projectValue(s, withAttributes(test.config, settings))}}, &resp)
			return resp.Diagnostics
		}
		t.Run(name, func(t *testing.T) {
			errs := validate(map[string]any{"commit_message": "Translated {{ component }}", "inherit_commit_message": true}).Errors()
			if len(errs) != 1 || !strings.Contains(errs[0].Detail(), "commit_message") {
				t.Fatalf("diagnostics = %v, want one error that names commit_message", errs)
			}
			if d, ok := errs[0].(diag.DiagnosticWithPath); !ok || !d.Path().Equal(path.Root("inherit_commit_message")) {
				t.Errorf("error is not at inherit_commit_message: %v", errs[0])
			}
			for _, settings := range []map[string]any{
				{"commit_message": "Translated", "inherit_commit_message": false},
				{"inherit_commit_message": true},
			} {
				if diags := validate(settings); diags.HasError() {
					t.Errorf("%v: diagnostics = %v, want none", settings, diags)
				}
			}
		})
	}
}

// inheritedJSON is the part of a Weblate response with the Inherited settings that the tests set: license and
// enforced_checks have their own value, commit_message is inherited, and pull_message keeps the trailing newline
// of Weblate's default.
const inheritedJSON = `"license": "MIT", "inherit_license": false, "enforced_checks": ["same", "max-length"],
	"inherit_enforced_checks": false, "secondary_language": 3, "inherit_secondary_language": false,
	"commit_message": "Translated", "inherit_commit_message": true, "pull_message": "Update\n",
	"inherit_pull_message": true`

func withInherited(body string) string {
	return strings.Replace(body, "{", "{"+inheritedJSON+", ", 1)
}

func enforcedChecks(checks ...string) []tftypes.Value {
	values := []tftypes.Value{}
	for _, c := range checks {
		values = append(values, str(c))
	}
	return values
}

func TestProjectPlanOfInheritedSettings(t *testing.T) {
	r, s := projectResourceWith(nil)
	modifyPlan := func(config, state map[string]any) resource.ModifyPlanResponse {
		plan := tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, withAttributes(state, config))}
		st := tfsdk.State{Schema: s.Schema, Raw: projectValue(s, state)}
		if state == nil {
			st.Raw = tftypes.NewValue(s.Schema.Type().TerraformType(context.Background()), nil)
		}
		resp := resource.ModifyPlanResponse{Plan: plan}
		r.(resource.ResourceWithModifyPlan).ModifyPlan(context.Background(), resource.ModifyPlanRequest{Plan: plan,
			State: st, Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)}}, &resp)
		return resp
	}
	base := map[string]any{"name": "Docs", "slug": "docs"}
	inherit := withAttributes(base, map[string]any{"inherit_license": true})

	t.Run("inherit without a Workspace fails", func(t *testing.T) {
		resp := modifyPlan(inherit, nil)
		wantAttributeError(t, resp.Diagnostics, "inherit_license")
		resp = modifyPlan(inherit, withAttributes(base, map[string]any{"id": "docs", "workspace": nil}))
		wantAttributeError(t, resp.Diagnostics, "inherit_license")
	})
	t.Run("inherit with a Workspace passes", func(t *testing.T) {
		withWorkspace := map[string]any{"workspace": "8f6e6f0c-1f7d-4d3e-9a3b-0c7f5d1e2a4b"}
		if resp := modifyPlan(withAttributes(inherit, withWorkspace), nil); resp.Diagnostics.HasError() {
			t.Errorf("configured Workspace: %v", resp.Diagnostics)
		}
		if resp := modifyPlan(inherit, withAttributes(base, withWorkspace)); resp.Diagnostics.HasError() {
			t.Errorf("Workspace in state: %v", resp.Diagnostics)
		}
	})
	t.Run("a set value plans the flag false", func(t *testing.T) {
		resp := modifyPlan(withAttributes(base, map[string]any{"license": "MIT"}),
			withAttributes(base, map[string]any{"id": "docs", "license": "MIT", "inherit_license": true}))
		if resp.Diagnostics.HasError() {
			t.Fatalf("ModifyPlan: %v", resp.Diagnostics)
		}
		var flag types.Bool
		resp.Diagnostics.Append(resp.Plan.GetAttribute(context.Background(), path.Root("inherit_license"), &flag)...)
		if !flag.Equal(types.BoolValue(false)) {
			t.Errorf("inherit_license = %v, want false", flag)
		}
	})
}

func TestComponentCreateSendsInheritedSettingsWithTheFlag(t *testing.T) {
	tree := referenceTree(withInherited(storedReference))
	config := withAttributes(referenceConfig, map[string]any{"license": "MIT",
		"enforced_checks": enforcedChecks("same", "max-length"), "inherit_commit_message": true})
	resp, requests := createComponent(t, func(r request) (int, string) {
		if r.method == http.MethodPost {
			return http.StatusCreated, withInherited(storedReference)
		}
		return tree(r)
	}, config)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
	body := requests[len(requests)-1].body
	for _, r := range requests {
		if r.method == http.MethodPost {
			body = r.body
		}
	}
	for name, want := range map[string]any{"license": "MIT", "inherit_license": false,
		"inherit_enforced_checks": false, "inherit_commit_message": true} {
		if body[name] != want {
			t.Errorf("%s = %v, want %v", name, body[name], want)
		}
	}
	if checks, ok := body["enforced_checks"].([]any); !ok || len(checks) != 2 {
		t.Errorf("enforced_checks = %v, want a list of both checks", body["enforced_checks"])
	}
	for _, name := range []string{"commit_message", "inherit_secondary_language", "pull_message"} {
		if _, sent := body[name]; sent {
			t.Errorf("Create sent %s, which the configuration omits", name)
		}
	}
}

func TestCategoryUpdateBackToTheStoredValueSendsItWithTheFlag(t *testing.T) {
	prior := map[string]any{"id": "docs/guides/api", "weblate_id": int64(9), "project": "docs",
		"category": "docs/guides", "name": "API", "slug": "api", "license": "MIT", "inherit_license": true,
		"commit_message": "Translated", "inherit_commit_message": false}
	config := map[string]any{"project": "docs", "category": "docs/guides", "name": "API", "slug": "api",
		"license": "MIT", "commit_message": "Translated"}
	stored := strings.Replace(withInherited(storedAPI), `"inherit_commit_message": true`, `"inherit_commit_message": false`, 1)
	tree := categoryTree(stored)
	c, requests := fakeWeblate(t, func(r request) (int, string) {
		if r.method == http.MethodPatch {
			return http.StatusOK, stored
		}
		return tree(r)
	})
	r, s := categoryResourceWith(c)
	plan := withAttributes(prior, map[string]any{"inherit_license": false})
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, plan)}}
	r.Update(context.Background(), resource.UpdateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, plan)},
		State:  tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	p := patches(*requests)
	if len(p) != 1 || len(p[0].body) != 2 || p[0].body["license"] != "MIT" || p[0].body["inherit_license"] != false {
		t.Errorf("requests = %v, want one PATCH with license and inherit_license = false", *requests)
	}
}

func TestProjectUpdateToInheritSendsOnlyTheFlag(t *testing.T) {
	prior := map[string]any{"id": "docs", "name": "Docs", "slug": "docs", "workspace": "w", "license": "MIT",
		"inherit_license": false}
	c, requests := fakeWeblate(t, func(request) (int, string) {
		return http.StatusOK, strings.Replace(storedProject, "{", `{"license": "MIT", "inherit_license": true, `, 1)
	})
	r, s := projectResourceWith(c)
	config := map[string]any{"name": "Docs", "slug": "docs", "workspace": "w", "inherit_license": true}
	plan := withAttributes(prior, config)
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, plan)}}
	r.Update(context.Background(), resource.UpdateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, plan)},
		State:  tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)},
	}, &resp)
	if len(*requests) != 1 || len((*requests)[0].body) != 1 || (*requests)[0].body["inherit_license"] != true {
		t.Errorf("requests = %v, want one PATCH with only inherit_license = true", *requests)
	}
}

func TestProjectReadStoresInheritedSettings(t *testing.T) {
	resp := readProjectResource(t, func(request) (int, string) { return http.StatusOK, withInherited(storedProject) },
		map[string]any{"id": "docs", "slug": "docs", "weblate_id": int64(7)})
	wantInheritedSettings(t, resp.State)
}

func TestProjectDataSourceReadsInheritedSettings(t *testing.T) {
	resp := readProject(t, "docs", http.StatusOK, withInherited(storedProject))
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	wantInheritedSettings(t, resp.State)
}

func wantInheritedSettings(t *testing.T, state tfsdk.State) {
	t.Helper()
	if v := stateAttribute[trimmedString](t, state, "license"); v.ValueString() != "MIT" {
		t.Errorf("license = %v", v)
	}
	if v := stateAttribute[types.Bool](t, state, "inherit_commit_message"); !v.ValueBool() {
		t.Errorf("inherit_commit_message = %v, want true", v)
	}
	if v := stateAttribute[types.Int64](t, state, "secondary_language"); v.ValueInt64() != 3 {
		t.Errorf("secondary_language = %v, want 3", v)
	}
	if v := stateAttribute[trimmedString](t, state, "pull_message"); v.ValueString() != "Update\n" {
		t.Errorf("pull_message = %q, want Weblate's value", v.ValueString())
	}
	checks := stateAttribute[types.Set](t, state, "enforced_checks")
	if want, _ := types.SetValueFrom(context.Background(), types.StringType, []string{"max-length", "same"}); !checks.Equal(want) {
		t.Errorf("enforced_checks = %v, want %v", checks, want)
	}
	if v := stateAttribute[types.Bool](t, state, "inherit_agreement"); !v.IsNull() {
		t.Errorf("inherit_agreement = %v, want null for a field missing from the response", v)
	}
}
