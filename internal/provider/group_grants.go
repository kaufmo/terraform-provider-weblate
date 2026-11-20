package provider

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

// grantAttributes lists the Grant attributes of weblate_group with their Grant type, in the order the provider
// writes them.
var grantAttributes = []struct {
	name  string
	grant client.GrantType
}{
	{"roles", client.RoleGrants},
	{"languages", client.LanguageGrants},
	{"projects", client.ProjectGrants},
	{"component_lists", client.ComponentListGrants},
	{"components", client.ComponentGrants},
}

// grantResolver translates between the Grants in Weblate's API, which are API URLs and numeric IDs, and the names in
// the configuration. It lists the Roles at most once.
type grantResolver struct {
	client *client.Client
	roles  []client.Role
}

func (gr *grantResolver) listRoles(ctx context.Context) ([]client.Role, error) {
	if gr.roles == nil {
		roles, err := gr.client.ListRoles(ctx)
		if err != nil {
			return nil, err
		}
		gr.roles = append([]client.Role{}, roles...)
	}
	return gr.roles, nil
}

// grantSets returns the Grants of g by attribute name, as sets of the names that the configuration uses.
func (gr *grantResolver) grantSets(ctx context.Context, g *client.Group) (map[string]types.Set, diag.Diagnostics) {
	var diags diag.Diagnostics
	sets := map[string]types.Set{}
	for _, a := range grantAttributes {
		names := []string{}
		for _, u := range g.Grants(a.grant) {
			name, d := gr.name(ctx, path.Root(a.name), a.grant, u)
			if diags.Append(d...); diags.HasError() {
				return nil, diags
			}
			names = append(names, name)
		}
		set, d := types.SetValueFrom(ctx, types.StringType, names)
		diags.Append(d...)
		sets[a.name] = set
	}
	return sets, diags
}

// name returns the name in the configuration of the Grant at the API URL apiURL.
func (gr *grantResolver) name(ctx context.Context, p path.Path, grant client.GrantType, apiURL string) (string, diag.Diagnostics) {
	var diags diag.Diagnostics
	var name string
	var err error
	switch grant {
	case client.RoleGrants:
		return gr.roleName(ctx, p, apiURL)
	case client.LanguageGrants:
		name, err = client.KeyFromURL(apiURL, "languages")
	case client.ProjectGrants:
		name, err = client.ProjectSlugFromURL(apiURL)
	case client.ComponentListGrants:
		name, err = client.KeyFromURL(apiURL, "component-lists")
	default:
		name, err = client.ComponentSlugPathFromURL(apiURL)
	}
	if err != nil {
		diags.AddAttributeError(p, "Weblate returned an unexpected Grant", err.Error())
	}
	return name, diags
}

func (gr *grantResolver) roleName(ctx context.Context, p path.Path, apiURL string) (string, diag.Diagnostics) {
	var diags diag.Diagnostics
	id, err := client.KeyFromURL(apiURL, "roles")
	if err != nil {
		diags.AddAttributeError(p, "Weblate returned an unexpected Grant", err.Error())
		return "", diags
	}
	roles, err := gr.listRoles(ctx)
	if err != nil {
		diags.Append(diag.WithPath(p, apiErrorDiagnostic("Listing the Roles failed", err)))
		return "", diags
	}
	for _, r := range roles {
		if strconv.FormatInt(r.ID, 10) == id {
			return r.Name, diags
		}
	}
	diags.AddAttributeError(p, "Role not visible",
		fmt.Sprintf("The Group grants the Role with ID %s, which the API token cannot see, so the provider cannot "+
			"name it. Give the token's user the role.view permission.", id))
	return "", diags
}

// key returns what the association endpoint of the Grant type takes for the object with the given name.
func (gr *grantResolver) key(ctx context.Context, p path.Path, grant client.GrantType, name string) (any, diag.Diagnostics) {
	var diags diag.Diagnostics
	var id int64
	var err error
	what := "Project"
	switch grant {
	case client.LanguageGrants:
		return name, diags
	case client.RoleGrants:
		roles, err := gr.listRoles(ctx)
		if err != nil {
			diags.Append(diag.WithPath(p, apiErrorDiagnostic("Listing the Roles failed", err)))
			return nil, diags
		}
		if i := slices.IndexFunc(roles, func(r client.Role) bool { return r.Name == name }); i >= 0 {
			return roles[i].ID, diags
		}
		diags.AddAttributeError(p, "Role not found",
			fmt.Sprintf("Weblate has no Role named %q that the API token can see. Without the role.view "+
				"permission, the token sees only the Roles of its own user's Groups.", name))
		return nil, diags
	case client.ProjectGrants:
		var project *client.Project
		if project, err = gr.client.GetProject(ctx, name); err == nil {
			id = project.ID
		}
	case client.ComponentListGrants:
		what = "Component list"
		var list *client.ComponentList
		if list, err = gr.client.GetComponentList(ctx, name); err == nil {
			id = list.ID
		}
	default:
		what = "Component"
		var component *client.Component
		if component, err = gr.client.GetComponent(ctx, client.ComponentRef(name)); err == nil {
			id = component.ID
		}
	}
	switch {
	case client.IsNotFound(err):
		diags.AddAttributeError(p, what+" not found", fmt.Sprintf("Weblate has no %s %s.", what, name))
	case err != nil:
		diags.Append(diag.WithPath(p, apiErrorDiagnostic("Reading the "+what+" "+name+" failed", err)))
	}
	return id, diags
}

// writeGrants changes the Grants of g, the Group as Weblate returned it, to planned, which holds the names by
// attribute. For each attribute it removes the Grants that are not planned, then adds the missing ones. It returns the
// Group as Weblate stores it afterwards, also after a failed step, so that state shows what was written.
func (gr *grantResolver) writeGrants(ctx context.Context, g *client.Group, planned map[string][]string) (*client.Group, diag.Diagnostics) {
	if len(planned) == 0 {
		return g, nil
	}
	var diags diag.Diagnostics
	for _, a := range grantAttributes {
		if want, ok := planned[a.name]; ok {
			if diags.Append(gr.writeGrant(ctx, g, a.name, a.grant, want)...); diags.HasError() {
				break
			}
		}
	}
	got, err := gr.client.GetGroup(ctx, g.ID)
	if err != nil {
		diags.Append(apiErrorDiagnostic("Reading the Group failed", err))
		return nil, diags
	}
	return got, diags
}

func (gr *grantResolver) writeGrant(ctx context.Context, g *client.Group, attribute string, grant client.GrantType, want []string) diag.Diagnostics {
	var diags diag.Diagnostics
	p := path.Root(attribute)
	have := map[string]bool{}
	for _, u := range g.Grants(grant) {
		name, d := gr.name(ctx, p, grant, u)
		if diags.Append(d...); diags.HasError() {
			return diags
		}
		have[name] = true
		if slices.Contains(want, name) {
			continue
		}
		key, d := gr.key(ctx, p, grant, name)
		if diags.Append(d...); diags.HasError() {
			return diags
		}
		if err := gr.client.RemoveGrant(ctx, g.ID, grant, key); err != nil && !client.IsNotFound(err) {
			diags.Append(diag.WithPath(p, apiErrorDiagnostic("Removing the Grant "+name+" failed", err)))
			return diags
		}
	}
	for _, name := range want {
		if have[name] {
			continue
		}
		key, d := gr.key(ctx, p, grant, name)
		if diags.Append(d...); diags.HasError() {
			return diags
		}
		if err := gr.client.AddGrant(ctx, g.ID, grant, key); err != nil {
			diags.Append(diag.WithPath(p, apiErrorDiagnostic("Adding the Grant "+name+" failed", err)))
			return diags
		}
	}
	return diags
}

// popGrants removes the Grant attributes from fields, the result of fieldsToWrite, and returns them by name.
func popGrants(fields map[string]any) map[string][]string {
	planned := map[string][]string{}
	for _, a := range grantAttributes {
		if names, ok := fields[a.name]; ok {
			planned[a.name] = []string{}
			for _, name := range names.([]any) {
				planned[a.name] = append(planned[a.name], name.(string))
			}
			delete(fields, a.name)
		}
	}
	return planned
}

// ValidateConfig rejects Grants that Weblate would clear or refuse with the configured selection modes and scope.
// Without it, the apply would fail halfway or the next plan would show the Grants as drift.
func (r *groupResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m groupResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	scoped := !m.Project.IsNull()
	if scoped {
		// Weblate answers 400 to any change of these on a Group that is scoped to a Project.
		const detail = "A Group with project set always grants its own Project, and Weblate cannot change %s on it. %s"
		if !m.Projects.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("projects"), "Projects on a Group scoped to a Project",
				fmt.Sprintf(detail, "the Projects", "Remove projects."))
		}
		if !m.ComponentLists.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("component_lists"),
				"Component lists on a Group scoped to a Project",
				fmt.Sprintf(detail, "the Component lists", "Remove component_lists."))
		}
		if isKnownOtherThan(m.ProjectSelection, "manual") {
			resp.Diagnostics.AddAttributeError(path.Root("project_selection"),
				"Project selection on a Group scoped to a Project",
				fmt.Sprintf(detail, "project_selection", `Set project_selection to "manual" or remove it.`))
		}
	}
	if !scoped && !m.Projects.IsNull() && isKnownOtherThan(m.ProjectSelection, "manual") {
		resp.Diagnostics.AddAttributeError(path.Root("projects"), "Projects that the Group does not use",
			fmt.Sprintf(`Weblate grants the granted Projects only with project_selection = "manual" and clears them `+
				`with %s. Set project_selection to "manual" or remove projects.`, m.ProjectSelection))
	}
	if !scoped && !m.ComponentLists.IsNull() && !m.ProjectSelection.IsUnknown() &&
		m.ProjectSelection.ValueString() != "component_list" {
		resp.Diagnostics.AddAttributeError(path.Root("component_lists"), "Component lists that the Group does not use",
			`Weblate grants Component lists only with project_selection = "component_list" and clears them `+
				`otherwise. Set project_selection to "component_list" or remove component_lists.`)
	}
	if !m.Languages.IsNull() && isKnownOtherThan(m.LanguageSelection, "manual") {
		resp.Diagnostics.AddAttributeError(path.Root("languages"), "Languages that the Group does not use",
			`With language_selection = "all", the Group grants all Languages. Set language_selection to "manual" `+
				`or remove languages.`)
	}
}

// ModifyPlan plans omitted projects and component_lists as unknown when project_selection changes, because Weblate's
// Group.save() then clears or recomputes them, and the prior value that UseStateForUnknown plans would not match.
func (r *groupResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var configuredProject, priorProject types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("project"), &configuredProject)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("project"), &priorProject)...)
	if configuredProject.IsNull() && !priorProject.IsNull() {
		// UseStateForUnknown planned the prior Project, which would hide the removal. Update rejects the planned
		// null; an error here would also block `-replace`, because OpenTofu plans with the prior state first.
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("project"), types.StringNull())...)
	}
	var planned, prior types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("project_selection"), &planned)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("project_selection"), &prior)...)
	if resp.Diagnostics.HasError() || planned.Equal(prior) {
		return
	}
	for _, attribute := range []string{"projects", "component_lists"} {
		var configured types.Set
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(attribute), &configured)...)
		if configured.IsNull() {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(attribute), types.SetUnknown(types.StringType))...)
		}
	}
}

// isKnownOtherThan reports whether the configuration sets v to a known value other than want.
func isKnownOtherThan(v types.String, want string) bool {
	return !v.IsNull() && !v.IsUnknown() && v.ValueString() != want
}
