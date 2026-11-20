package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

// planSlugPath rejects a parent Category outside the Project and plans id as the new slug path, for a resource with
// the attributes project, category and slug, so that a rename or move shows the new id instead of
// "known after apply".
func planSlugPath(ctx context.Context, resp *resource.ModifyPlanResponse) {
	var project, parent types.String
	var slug trimmedString
	resp.Diagnostics.Append(resp.Plan.GetAttribute(ctx, path.Root("project"), &project)...)
	resp.Diagnostics.Append(resp.Plan.GetAttribute(ctx, path.Root("category"), &parent)...)
	resp.Diagnostics.Append(resp.Plan.GetAttribute(ctx, path.Root("slug"), &slug)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if parent.IsNull() {
		parent = project
	} else if !project.IsUnknown() && !parent.IsUnknown() &&
		!strings.HasPrefix(parent.ValueString(), project.ValueString()+"/") {
		resp.Diagnostics.AddAttributeError(path.Root("category"), "Parent Category outside the Project",
			fmt.Sprintf("The parent Category %s is not in the Project %s. Set category to the id of a Category "+
				"in that Project.", parent.ValueString(), project.ValueString()))
		return
	}
	if parent.IsUnknown() || slug.IsUnknown() {
		return
	}
	id := parent.ValueString() + "/" + strings.TrimSpace(slug.ValueString())
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("id"), id)...)
}

// parentFromSlugPath returns the category attribute of an object with the given slug path: the slug path of its
// Category, or null at the Project root.
func parentFromSlugPath(slugPath string, inCategory bool) types.String {
	if !inCategory {
		return types.StringNull()
	}
	return types.StringValue(slugPath[:strings.LastIndex(slugPath, "/")])
}

// categoryURL returns the API URL of the Category with the given slug path, the form in which Weblate takes a
// Category reference.
func categoryURL(ctx context.Context, c *client.Client, slugPath types.String, diags *diag.Diagnostics) any {
	cat, err := c.FindCategory(ctx, slugPath.ValueString())
	if err != nil {
		diags.Append(apiErrorDiagnostic("Finding the Category failed", err))
		return nil
	}
	if cat == nil {
		diags.AddAttributeError(path.Root("category"), "Category not found",
			fmt.Sprintf("Weblate has no Category with the slug path %s.", slugPath.ValueString()))
		return nil
	}
	return cat.URL
}

// sameProject reports whether the object, which Weblate has in the Project current, stays where it is with the
// planned project. Slugs are unique, so the same slug means the same Project, renamed since the last refresh. For
// another Project it adds an error, because the API cannot move objects between Projects (ADR 0002); loss says what
// -replace deletes.
func sameProject(diags *diag.Diagnostics, object, id, current, planned, loss string) bool {
	if current == planned {
		return true
	}
	diags.AddAttributeError(path.Root("project"), object+" cannot change its Project",
		fmt.Sprintf("The %[1]s %[2]s is in the Project %[3]s, and Weblate cannot move a %[1]s to another "+
			"Project. To delete it there and create it in the Project %[4]s, which deletes %[5]s, apply with "+
			"-replace for this resource.", object, id, current, planned, loss))
	return false
}
