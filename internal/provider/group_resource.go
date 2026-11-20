package provider

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

var (
	_ resource.ResourceWithConfigure      = &groupResource{}
	_ resource.ResourceWithImportState    = &groupResource{}
	_ resource.ResourceWithValidateConfig = &groupResource{}
	_ resource.ResourceWithModifyPlan     = &groupResource{}
)

// selections maps the names of the selection attributes to the numbers that Weblate uses for them on the wire.
var selections = map[string]map[string]int64{
	"project_selection":  {"manual": 0, "all": 1, "component_list": 2, "all_public": 3, "all_protected": 4},
	"language_selection": {"manual": 0, "all": 1},
}

type groupResource struct {
	resourceClient
}

type groupResourceModel struct {
	ID                types.String  `tfsdk:"id"`
	Name              trimmedString `tfsdk:"name"`
	Project           types.String  `tfsdk:"project"`
	ProjectSelection  types.String  `tfsdk:"project_selection"`
	LanguageSelection types.String  `tfsdk:"language_selection"`
	Enforced2FA       types.Bool    `tfsdk:"enforced_2fa"`
	Roles             types.Set     `tfsdk:"roles"`
	Languages         types.Set     `tfsdk:"languages"`
	Projects          types.Set     `tfsdk:"projects"`
	ComponentLists    types.Set     `tfsdk:"component_lists"`
	Components        types.Set     `tfsdk:"components"`
}

func newGroupResourceModel(ctx context.Context, g *client.Group, gr *grantResolver) (groupResourceModel, diag.Diagnostics) {
	grants, diags := gr.grantSets(ctx, g)
	if diags.HasError() {
		return groupResourceModel{}, diags
	}
	m := groupResourceModel{
		Roles:          grants["roles"],
		Languages:      grants["languages"],
		Projects:       grants["projects"],
		ComponentLists: grants["component_lists"],
		Components:     grants["components"],
		ID:             types.StringValue(strconv.FormatInt(g.ID, 10)),
		Name:           trimmedStringValue(g.Name),
		Project:        types.StringNull(),
		Enforced2FA:    types.BoolValue(g.Enforced2FA),
	}
	if g.DefiningProject != nil {
		slug, err := client.ProjectSlugFromURL(*g.DefiningProject)
		if err != nil {
			diags.AddError("Weblate returned an unexpected Project", err.Error())
		}
		m.Project = types.StringValue(slug)
	}
	m.ProjectSelection = selectionName(&diags, "project_selection", g.ProjectSelection)
	m.LanguageSelection = selectionName(&diags, "language_selection", g.LanguageSelection)
	return m, diags
}

func selectionName(diags *diag.Diagnostics, attribute string, number int64) types.String {
	for name, n := range selections[attribute] {
		if n == number {
			return types.StringValue(name)
		}
	}
	diags.AddAttributeError(path.Root(attribute), "Weblate returned an unknown selection",
		fmt.Sprintf("Weblate returned %d for %s, which this version of the provider does not know. Upgrade the "+
			"provider.", number, attribute))
	return types.StringNull()
}

func NewGroupResource() resource.Resource {
	return &groupResource{}
}

func (r *groupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (r *groupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	selection := func(attribute, description string) schema.Attribute {
		return schema.StringAttribute{
			MarkdownDescription: description + " Weblate's default is `manual`.",
			Optional:            true,
			Computed:            true,
			Validators:          []validator.String{oneOfValidator{slices.Sorted(maps.Keys(selections[attribute]))}},
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}
	grants := func(description string) schema.Attribute {
		return schema.SetAttribute{
			MarkdownDescription: description + " When set, even to `[]`, the attribute lists all Grants of its " +
				"type: Grants added outside OpenTofu show as drift and the next apply removes them. When omitted, " +
				"the provider reads the Grants but never changes them, and removing the attribute from the " +
				"configuration leaves the Grants in Weblate.",
			ElementType:   types.StringType,
			Optional:      true,
			Computed:      true,
			PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Weblate Group: a set of users that holds Grants, either for the whole Weblate " +
			"instance or scoped to one Project.\n\n" +
			"Managing Groups needs a User token of a user who may manage Groups; Weblate answers a Project token with " +
			"`403`. An attribute that the configuration omits is not managed: the provider reads it but never " +
			"changes it. After create and update, the provider compares every configured attribute with the value " +
			"Weblate stored and fails the apply when they differ.\n\n" +
			"Weblate creates Built-in Groups, such as Administration or Review, for every Project. They can be " +
			"imported, but Weblate's API does not tell them apart from other Groups, so destroying the resource " +
			"deletes the Built-in Group. To stop managing an imported Built-in Group, use a `removed` block or " +
			"`tofu state rm` instead.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The numeric ID of the Group.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the Group.",
				CustomType:          trimmedStringType{},
				Required:            true,
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "Slug of the Project that the Group is scoped to. Without it, a new Group " +
					"belongs to the whole Weblate instance. Weblate cannot move an existing Group to another Project " +
					"or to the whole instance, so changing or removing `project` fails on apply, unless the Project " +
					"was only renamed. To move a Group, replace it with `-replace`.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"project_selection": selection("project_selection", "Which Projects the Group grants access to: "+
				"`manual` only the granted Projects, `all` all Projects, `all_public` all public Projects, "+
				"`all_protected` all protected Projects, `component_list` the Projects of the granted Component "+
				"lists. Weblate sets `manual` on a Group that is scoped to a Project and rejects any change of it."),
			"language_selection": selection("language_selection", "Which Languages the Group grants access to: "+
				"`manual` only the granted Languages, `all` all Languages."),
			"enforced_2fa": schema.BoolAttribute{
				MarkdownDescription: "Whether members of the Group need two-factor authentication.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"roles": grants("Names of the Roles that the Group grants, such as `Review strings`. The provider " +
				"looks up their IDs with the API token, which needs the `role.view` permission to see Roles " +
				"outside its own Groups. When the last apply set `roles`, a Role that the token cannot see fails " +
				"the refresh; otherwise, such as after an import, the provider leaves it out with a warning."),
			"languages": grants("Codes of the Languages that the Group grants, such as `de`. Needs " +
				"`language_selection = \"manual\"`."),
			"projects": grants("Slugs of the Projects that the Group grants. Needs `project_selection = " +
				"\"manual\"` and a Group without `project`. On a Group with `project`, Weblate grants that Project " +
				"and the provider reads it into this attribute."),
			"component_lists": grants("Slugs of the Component lists that the Group grants. Needs " +
				"`project_selection = \"component_list\"` and a Group without `project`."),
			"components": grants("Slug paths of the Components that the Group grants, such as `docs/guides/api`. " +
				"On a Group with `project`, only Components of that Project."),
		},
	}
}

// apiFields turns the attributes from fieldsToWrite into the fields of Weblate's Group serializer.
func (r *groupResource) apiFields(fields map[string]any) map[string]any {
	if slug, ok := fields["project"]; ok {
		delete(fields, "project")
		fields["defining_project"] = r.client.ProjectURL(slug.(string))
	}
	for attribute, numbers := range selections {
		if name, ok := fields[attribute]; ok {
			fields[attribute] = numbers[name.(string)]
		}
	}
	return fields
}

func (r *groupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	fields, diags := fieldsToWrite(ctx, req.Config, req.Plan, nil)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	planned := popGrants(fields)
	g, err := r.client.CreateGroup(ctx, r.apiFields(fields))
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Creating the Group failed", err))
		return
	}
	r.writeGrantsAndStore(ctx, req.Config, g, planned, &resp.State, resp.Private, &resp.Diagnostics)
}

func (r *groupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	id, diags := groupID(ctx, req.State)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	g, err := r.client.GetGroup(ctx, id)
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Reading the Group failed", err))
		return
	}
	rolesConfigured, diags := req.Private.GetKey(ctx, rolesConfiguredKey)
	resp.Diagnostics.Append(diags...)
	m, diags := newGroupResourceModel(ctx, g, &grantResolver{client: r.client, skipHiddenRoles: len(rolesConfigured) == 0})
	resp.Diagnostics.Append(diags...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
}

func (r *groupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	fields, diags := fieldsToWrite(ctx, req.Config, req.Plan, &req.State)
	resp.Diagnostics.Append(diags...)
	id, diags := groupID(ctx, req.State)
	resp.Diagnostics.Append(diags...)
	var plannedProject, priorProject types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("project"), &plannedProject)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("project"), &priorProject)...)
	if plannedProject.IsNull() && !priorProject.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("project"), "Cannot move the Group to the whole Weblate instance",
			fmt.Sprintf("The Group belongs to the Project %s, and Weblate's API cannot change the Project of an "+
				"existing Group. Set project = %q, or replace the Group with `-replace` to make it instance-wide.",
				priorProject.ValueString(), priorProject.ValueString()))
	}
	if resp.Diagnostics.HasError() {
		return
	}
	planned := popGrants(fields)
	var g *client.Group
	var err error
	if project, ok := fields["project"]; ok {
		// Weblate rejects any change of defining_project, but a planned change can also be a rename of the
		// Project, which Weblate already shows in the Group.
		delete(fields, "project")
		if g, err = r.client.GetGroup(ctx, id); err != nil {
			resp.Diagnostics.Append(apiErrorDiagnostic("Reading the Group failed", err))
			return
		}
		if d := projectChangeDiagnostics(g, project.(string)); d.HasError() {
			resp.Diagnostics.Append(d...)
			return
		}
	}
	// The PATCH goes first, because Group.save() in Weblate clears Project and Component list Grants depending on
	// the selection modes.
	switch {
	case len(fields) > 0:
		g, err = r.client.UpdateGroup(ctx, id, r.apiFields(fields))
	case g == nil:
		g, err = r.client.GetGroup(ctx, id)
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Updating the Group failed", err))
		return
	}
	r.writeGrantsAndStore(ctx, req.Config, g, planned, &resp.State, resp.Private, &resp.Diagnostics)
}

func projectChangeDiagnostics(g *client.Group, planned string) diag.Diagnostics {
	var diags diag.Diagnostics
	current := "the whole Weblate instance"
	if g.DefiningProject != nil {
		slug, err := client.ProjectSlugFromURL(*g.DefiningProject)
		if err != nil {
			diags.AddError("Weblate returned an unexpected Project", err.Error())
			return diags
		}
		if slug == planned {
			return diags
		}
		current = "the Project " + slug
	}
	diags.AddAttributeError(path.Root("project"), "Cannot move the Group to another Project",
		fmt.Sprintf("The Group belongs to %s, and Weblate's API cannot change the Project of an existing Group. "+
			"To move the Group to the Project %s, replace it with `-replace`.", current, planned))
	return diags
}

// writeGrantsAndStore writes the planned Grants to g, the Group that Weblate returned for a create or update, and puts
// the Group into state, also when a Grant or the read-back check fails, because the writes before happened.
func (r *groupResource) writeGrantsAndStore(ctx context.Context, config tfsdk.Config, g *client.Group,
	planned map[string][]string, state *tfsdk.State, private privateState, diags *diag.Diagnostics) {
	var roles types.Set
	diags.Append(config.GetAttribute(ctx, path.Root("roles"), &roles)...)
	rolesConfigured := []byte(nil)
	if !roles.IsNull() {
		rolesConfigured = []byte("true")
	}
	diags.Append(private.SetKey(ctx, rolesConfiguredKey, rolesConfigured)...)
	gr := &grantResolver{client: r.client, skipHiddenRoles: roles.IsNull()}
	g, written := gr.writeGrants(ctx, g, planned)
	diags.Append(written...)
	if g == nil {
		return
	}
	m, d := newGroupResourceModel(ctx, g, gr)
	if diags.Append(d...); d.HasError() {
		return
	}
	d = state.Set(ctx, m)
	if diags.Append(d...); !d.HasError() && !written.HasError() {
		diags.Append(readBackDiagnostics(ctx, config, *state)...)
	}
}

func (r *groupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	id, diags := groupID(ctx, req.State)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteGroup(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.Append(apiErrorDiagnostic("Deleting the Group failed", err))
	}
}

func (r *groupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := strconv.ParseInt(req.ID, 10, 64); err != nil {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("The import ID of a weblate_group is the numeric ID of the Group, such as 12, not %q.", req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func groupID(ctx context.Context, state tfsdk.State) (int64, diag.Diagnostics) {
	var id types.String
	diags := state.GetAttribute(ctx, path.Root("id"), &id)
	if diags.HasError() {
		return 0, diags
	}
	n, err := strconv.ParseInt(id.ValueString(), 10, 64)
	if err != nil {
		diags.AddAttributeError(path.Root("id"), "Invalid Group ID", err.Error())
	}
	return n, diags
}

type oneOfValidator struct {
	values []string
}

func (v oneOfValidator) Description(context.Context) string {
	return "value must be one of: " + strings.Join(v.values, ", ")
}

func (v oneOfValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v oneOfValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || slices.Contains(v.values, req.ConfigValue.ValueString()) {
		return
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Invalid value",
		fmt.Sprintf("%q is not valid: %s.", req.ConfigValue.ValueString(), v.Description(ctx)))
}
