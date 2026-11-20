package provider

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const twoRoles = `{"count": 2, "next": null, "previous": null, "results": [
	{"id": 1, "name": "Administration", "permissions": ["vcs.view", "project.edit"]},
	{"id": 4, "name": "Translate", "permissions": ["unit.edit"]}]}`

func readRole(t *testing.T, name string) datasource.ReadResponse {
	t.Helper()
	return readDataSource(t, NewRoleDataSource(), "/api/roles/", "name", name, http.StatusOK, twoRoles)
}

func TestRoleDataSourceReadsTheRoleWithTheName(t *testing.T) {
	resp := readRole(t, "Translate")
	ctx := context.Background()
	var id types.Int64
	var permissions []string
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("id"), &id)...)
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("permissions"), &permissions)...)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	if id.ValueInt64() != 4 {
		t.Errorf("id = %v, want 4", id)
	}
	if !slices.Equal(permissions, []string{"unit.edit"}) {
		t.Errorf("permissions = %v, want [unit.edit]", permissions)
	}
}

func TestRoleDataSourceNamesTheMissingRole(t *testing.T) {
	resp := readRole(t, "Reviewer")
	errs := resp.Diagnostics.Errors()
	if len(errs) != 1 {
		t.Fatalf("diagnostics = %v, want one error", resp.Diagnostics)
	}
	if !strings.Contains(errs[0].Summary()+errs[0].Detail(), `"Reviewer"`) {
		t.Errorf("error %q / %q does not name the Role", errs[0].Summary(), errs[0].Detail())
	}
}

func TestProviderRegistersRoleDataSource(t *testing.T) {
	ctx := context.Background()
	for _, factory := range New("test")().DataSources(ctx) {
		var resp datasource.MetadataResponse
		factory().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "weblate"}, &resp)
		if resp.TypeName == "weblate_role" {
			return
		}
	}
	t.Error("the provider has no weblate_role data source")
}
