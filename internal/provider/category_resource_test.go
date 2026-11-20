package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

// The Project docs holds the Category guides (ID 4), which holds the Category api (ID 9). Weblate links them by
// absolute API URLs on its own site domain.
const (
	storedGuides = `{"id": 4, "slug": "guides", "name": "Guides", "project": "http://weblate.example/api/projects/docs/",
		"category": null, "url": "http://weblate.example/api/categories/4/"}`
	storedAPI = `{"id": 9, "slug": "api", "name": "API", "project": "http://weblate.example/api/projects/docs/",
		"category": "http://weblate.example/api/categories/4/", "url": "http://weblate.example/api/categories/9/"}`
	storedAPIAtRoot = `{"id": 9, "slug": "api", "name": "API", "project": "http://weblate.example/api/projects/docs/",
		"category": null, "url": "http://weblate.example/api/categories/9/"}`
	notFound = `{"detail": "No Category matches the given query."}`
)

// categoryTree answers like a Weblate instance that holds docs/guides and docs/guides/api, with api answered by
// apiBody for every request on it.
func categoryTree(apiBody string) func(r request) (int, string) {
	return func(r request) (int, string) {
		switch r.path {
		case "/api/categories/4/":
			return http.StatusOK, storedGuides
		case "/api/categories/9/":
			return http.StatusOK, apiBody
		case "/api/projects/docs/categories/":
			return http.StatusOK, `{"next": null, "results": [` + storedGuides + `, ` + apiBody + `]}`
		}
		return http.StatusNotFound, notFound
	}
}

func categoryResourceWith(c *client.Client) (resource.ResourceWithConfigure, resource.SchemaResponse) {
	ctx := context.Background()
	r := NewCategoryResource().(resource.ResourceWithConfigure)
	r.Configure(ctx, resource.ConfigureRequest{ProviderData: c}, &resource.ConfigureResponse{})
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	return r, schemaResp
}

func createCategory(t *testing.T, respond func(r request) (int, string), config map[string]any) (resource.CreateResponse, []request) {
	t.Helper()
	c, requests := fakeWeblate(t, respond)
	r, s := categoryResourceWith(c)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, nil)}}
	r.Create(context.Background(), resource.CreateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, config)},
	}, &resp)
	return resp, *requests
}

func TestCategoryCreateInsideAParentCategory(t *testing.T) {
	tree := categoryTree(storedAPI)
	resp, requests := createCategory(t, func(r request) (int, string) {
		if r.method == http.MethodPost {
			return http.StatusCreated, storedAPI
		}
		return tree(r)
	}, map[string]any{"project": "docs", "category": "docs/guides", "name": "API", "slug": "api"})
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}

	var post *request
	for i := range requests {
		if requests[i].method == http.MethodPost {
			post = &requests[i]
		}
	}
	if post == nil || post.path != "/api/categories/" {
		t.Fatalf("requests = %v, want a POST /api/categories/", requests)
	}
	if got := post.body; len(got) != 4 || got["name"] != "API" || got["slug"] != "api" ||
		!strings.HasSuffix(got["project"].(string), "/api/projects/docs/") ||
		got["category"] != "http://weblate.example/api/categories/4/" {
		t.Errorf("body = %v, want name, slug and the API URLs of the Project and the parent Category", got)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "docs/guides/api" {
		t.Errorf("id = %v, want docs/guides/api", id)
	}
	if id := stateAttribute[types.Int64](t, resp.State, "weblate_id"); id.ValueInt64() != 9 {
		t.Errorf("weblate_id = %v", id)
	}
}

func TestCategoryCreateAtTheRootSendsNoParent(t *testing.T) {
	resp, requests := createCategory(t, func(r request) (int, string) {
		if r.method == http.MethodPost {
			return http.StatusCreated, storedGuides
		}
		return http.StatusNotFound, notFound
	}, map[string]any{"project": "docs", "name": "Guides", "slug": "guides"})
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
	if len(requests) != 1 || len(requests[0].body) != 3 {
		t.Errorf("requests = %v, want one POST with name, slug and project", requests)
	}
	if c := stateAttribute[types.String](t, resp.State, "category"); !c.IsNull() {
		t.Errorf("category = %v, want null", c)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "docs/guides" {
		t.Errorf("id = %v, want docs/guides", id)
	}
}

func TestCategoryCreateFailsWhenTheParentDoesNotExist(t *testing.T) {
	resp, requests := createCategory(t, categoryTree(storedAPI),
		map[string]any{"project": "docs", "category": "docs/missing", "name": "API", "slug": "api"})

	errs := resp.Diagnostics.Errors()
	if len(errs) != 1 || !strings.Contains(errs[0].Detail(), "docs/missing") {
		t.Fatalf("diagnostics = %v, want one error naming docs/missing", resp.Diagnostics)
	}
	for _, r := range requests {
		if r.method == http.MethodPost {
			t.Errorf("Create sent %v", r)
		}
	}
}

func readCategoryResource(t *testing.T, respond func(r request) (int, string), state map[string]any) resource.ReadResponse {
	t.Helper()
	c, _ := fakeWeblate(t, respond)
	r, s := categoryResourceWith(c)
	resp := resource.ReadResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, state)}}
	r.Read(context.Background(), resource.ReadRequest{State: resp.State}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	return resp
}

func TestCategoryReadByWeblateIDShowsAMoveAsDrift(t *testing.T) {
	resp := readCategoryResource(t, categoryTree(storedAPIAtRoot),
		map[string]any{"id": "docs/guides/api", "weblate_id": int64(9), "project": "docs", "category": "docs/guides"})

	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "docs/api" {
		t.Errorf("id = %v, want docs/api", id)
	}
	if c := stateAttribute[types.String](t, resp.State, "category"); !c.IsNull() {
		t.Errorf("category = %v, want null", c)
	}
	if p := stateAttribute[types.String](t, resp.State, "project"); p.ValueString() != "docs" {
		t.Errorf("project = %v, want docs", p)
	}
}

func TestCategoryReadRemovesADeletedCategory(t *testing.T) {
	resp := readCategoryResource(t, func(request) (int, string) { return http.StatusNotFound, notFound },
		map[string]any{"id": "docs/guides", "weblate_id": int64(4)})
	if !resp.State.Raw.IsNull() {
		t.Errorf("state = %v, want the resource removed", resp.State.Raw)
	}
}

func updateCategory(t *testing.T, respond func(r request) (int, string), prior, config map[string]any) (resource.UpdateResponse, []request) {
	t.Helper()
	c, requests := fakeWeblate(t, respond)
	r, s := categoryResourceWith(c)
	plan := map[string]any{"id": tftypes.UnknownValue, "weblate_id": prior["weblate_id"]}
	for k, v := range config {
		plan[k] = v
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)}}
	r.Update(context.Background(), resource.UpdateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, plan)},
		State:  tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)},
	}, &resp)
	return resp, *requests
}

func patches(requests []request) []request {
	var found []request
	for _, r := range requests {
		if r.method == http.MethodPatch {
			found = append(found, r)
		}
	}
	return found
}

func TestCategoryUpdateMovesToTheRootWithAnExplicitNull(t *testing.T) {
	tree := categoryTree(storedAPI)
	resp, requests := updateCategory(t, func(r request) (int, string) {
		if r.method == http.MethodPatch {
			return http.StatusOK, storedAPIAtRoot
		}
		return tree(r)
	},
		map[string]any{"id": "docs/guides/api", "weblate_id": int64(9), "project": "docs", "category": "docs/guides",
			"name": "API", "slug": "api"},
		map[string]any{"project": "docs", "name": "API", "slug": "api"})
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}

	p := patches(requests)
	if len(p) != 1 || p[0].path != "/api/categories/9/" {
		t.Fatalf("requests = %v, want one PATCH /api/categories/9/", requests)
	}
	if v, ok := p[0].body["category"]; len(p[0].body) != 1 || !ok || v != nil {
		t.Errorf("body = %v, want only category: null", p[0].body)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "docs/api" {
		t.Errorf("id = %v, want docs/api", id)
	}
}

func TestCategoryUpdateMovesIntoAnotherParent(t *testing.T) {
	tree := categoryTree(storedAPIAtRoot)
	resp, requests := updateCategory(t, func(r request) (int, string) {
		if r.method == http.MethodPatch {
			return http.StatusOK, storedAPI
		}
		return tree(r)
	},
		map[string]any{"id": "docs/api", "weblate_id": int64(9), "project": "docs", "name": "API", "slug": "api"},
		map[string]any{"project": "docs", "category": "docs/guides", "name": "API", "slug": "api"})
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	if p := patches(requests); len(p) != 1 || len(p[0].body) != 1 ||
		p[0].body["category"] != "http://weblate.example/api/categories/4/" {
		t.Errorf("requests = %v, want one PATCH with the parent's API URL", requests)
	}
}

func TestCategoryUpdateToAnotherProjectFailsWithTheReplaceHint(t *testing.T) {
	resp, requests := updateCategory(t, categoryTree(storedGuides),
		map[string]any{"id": "docs/guides", "weblate_id": int64(4), "project": "docs", "name": "Guides", "slug": "guides"},
		map[string]any{"project": "other", "name": "Guides", "slug": "guides"})

	errs := resp.Diagnostics.Errors()
	if len(errs) != 1 || !strings.Contains(errs[0].Detail(), "-replace") {
		t.Fatalf("diagnostics = %v, want one error that points to -replace", resp.Diagnostics)
	}
	if p := patches(requests); len(p) != 0 {
		t.Errorf("Update sent %v", p)
	}
}

func TestCategoryUpdateAfterAProjectRenameOnlyChangesState(t *testing.T) {
	renamed := strings.ReplaceAll(storedGuides, "/projects/docs/", "/projects/handbook/")
	resp, requests := updateCategory(t, func(r request) (int, string) { return http.StatusOK, renamed },
		map[string]any{"id": "docs/guides", "weblate_id": int64(4), "project": "docs", "name": "Guides", "slug": "guides"},
		map[string]any{"project": "handbook", "name": "Guides", "slug": "guides"})
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	if p := patches(requests); len(p) != 0 {
		t.Errorf("Update sent %v", p)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "handbook/guides" {
		t.Errorf("id = %v, want handbook/guides", id)
	}
}

func modifyCategoryPlan(t *testing.T, plan map[string]any) resource.ModifyPlanResponse {
	t.Helper()
	r, s := categoryResourceWith(nil)
	p := tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, plan)}
	resp := resource.ModifyPlanResponse{Plan: p}
	r.(resource.ResourceWithModifyPlan).ModifyPlan(context.Background(), resource.ModifyPlanRequest{Plan: p,
		Config: tfsdk.Config{Schema: s.Schema, Raw: p.Raw}}, &resp)
	return resp
}

func TestCategoryPlanSetsIDToTheNewSlugPath(t *testing.T) {
	for _, tc := range []struct {
		plan map[string]any
		want string
	}{
		{map[string]any{"project": "docs", "slug": "guides"}, "docs/guides"},
		{map[string]any{"project": "docs", "category": "docs/guides", "slug": "api"}, "docs/guides/api"},
	} {
		tc.plan["id"] = tftypes.UnknownValue
		resp := modifyCategoryPlan(t, tc.plan)
		if resp.Diagnostics.HasError() {
			t.Fatalf("ModifyPlan: %v", resp.Diagnostics)
		}
		var id types.String
		resp.Diagnostics.Append(resp.Plan.GetAttribute(context.Background(), path.Root("id"), &id)...)
		if id.ValueString() != tc.want {
			t.Errorf("planned id = %v, want %s", id, tc.want)
		}
	}
}

func TestCategoryPlanRejectsAParentOutsideTheProject(t *testing.T) {
	resp := modifyCategoryPlan(t, map[string]any{"id": tftypes.UnknownValue, "project": "docs",
		"category": "docs-old/guides", "slug": "api"})
	if errs := resp.Diagnostics.Errors(); len(errs) != 1 || !strings.Contains(errs[0].Detail(), "docs-old/guides") {
		t.Errorf("diagnostics = %v, want one error naming the parent", resp.Diagnostics)
	}
}

func TestCategoryImportResolvesTheSlugPath(t *testing.T) {
	c, _ := fakeWeblate(t, categoryTree(storedAPI))
	r, s := categoryResourceWith(c)
	resp := resource.ImportStateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, nil)}}
	r.(resource.ResourceWithImportState).ImportState(context.Background(),
		resource.ImportStateRequest{ID: "docs/guides/api"}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("ImportState: %v", resp.Diagnostics)
	}
	if id := stateAttribute[types.Int64](t, resp.State, "weblate_id"); id.ValueInt64() != 9 {
		t.Errorf("weblate_id = %v, want 9", id)
	}

	resp = resource.ImportStateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, nil)}}
	r.(resource.ResourceWithImportState).ImportState(context.Background(),
		resource.ImportStateRequest{ID: "docs/missing"}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Error("ImportState of docs/missing succeeded")
	}
}

func TestCategoryDeleteWaitsUntilTheCategoryIsGone(t *testing.T) {
	c, requests := fakeWeblate(t, func(r request) (int, string) {
		if r.method == http.MethodDelete {
			return http.StatusNoContent, ""
		}
		return http.StatusNotFound, notFound
	})
	r, s := categoryResourceWith(c)
	state := tfsdk.State{Schema: s.Schema, Raw: projectValue(s, map[string]any{"id": "docs/guides", "weblate_id": int64(4)})}

	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}
	if len(*requests) != 2 || (*requests)[0].method != http.MethodDelete || (*requests)[0].path != "/api/categories/4/" ||
		(*requests)[1].method != http.MethodGet || (*requests)[1].path != "/api/categories/4/" {
		t.Errorf("requests = %v, want DELETE and GET /api/categories/4/", *requests)
	}
}

func TestProviderRegistersCategoryResource(t *testing.T) {
	ctx := context.Background()
	for _, factory := range New("test")().Resources(ctx) {
		var resp resource.MetadataResponse
		factory().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "weblate"}, &resp)
		if resp.TypeName == "weblate_category" {
			return
		}
	}
	t.Error("the provider has no weblate_category resource")
}
