package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

var (
	_ resource.ResourceWithConfigure      = &addonResource{}
	_ resource.ResourceWithImportState    = &addonResource{}
	_ resource.ResourceWithValidateConfig = &addonResource{}
)

type addonResource struct {
	client *client.Client
}

type addonResourceModel struct {
	ID            types.String         `tfsdk:"id"`
	Component     types.String         `tfsdk:"component"`
	Project       types.String         `tfsdk:"project"`
	Name          types.String         `tfsdk:"name"`
	Configuration jsontypes.Normalized `tfsdk:"configuration"`
}

func newAddonResourceModel(a *client.Addon) (addonResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	m := addonResourceModel{
		ID:        types.StringValue(strconv.FormatInt(a.ID, 10)),
		Component: types.StringNull(),
		Project:   types.StringNull(),
		Name:      types.StringValue(a.Name),
	}
	var err error
	switch {
	case a.Component != nil:
		var slugPath string
		slugPath, err = client.ComponentSlugPathFromURL(*a.Component)
		m.Component = types.StringValue(slugPath)
	case a.Project != nil:
		var slug string
		slug, err = client.ProjectSlugFromURL(*a.Project)
		m.Project = types.StringValue(slug)
	}
	if err != nil {
		diags.AddError("Weblate returned an unexpected Component or Project", err.Error())
	}
	configuration, err := componentField{kind: jsonField}.value(a.Configuration)
	if err != nil {
		diags.AddAttributeError(path.Root("configuration"), "Unexpected value from Weblate",
			fmt.Sprintf("Weblate returned %s for configuration: %s.", a.Configuration, err))
	}
	m.Configuration = configuration.(jsontypes.Normalized)
	return m, diags
}

// NewAddonResource returns the weblate_addon resource.
func NewAddonResource() resource.Resource {
	return &addonResource{}
}

func (r *addonResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_addon"
}

func (r *addonResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Weblate Add-on on a Component or a Project.\n\n" +
			"The provider does not check `name` or the keys of `configuration` at plan; Weblate rejects them on " +
			"apply. Weblate stores only the configuration keys that the Add-on knows and drops the others without " +
			"an error. After create and update, the provider compares `configuration` with the value Weblate " +
			"stored and fails the apply with the keys that Weblate dropped or changed.\n\n" +
			"Weblate installs some Add-ons on its own when it creates a Component, depending on the file format " +
			"and the `DEFAULT_ADDONS` setting. When Weblate answers that the Add-on is already installed on the " +
			"Component or Project, create takes over the installed Add-on and sends `configuration` when it is " +
			"set. Add-ons that Weblate allows more than once, such as `weblate.webhook.webhook`, are always " +
			"installed again. Two `weblate_addon` resources with the same `name` on the same Component or Project " +
			"therefore take over the same Add-on and overwrite each other's `configuration`.\n\n" +
			"Weblate cannot change the name of an Add-on or move it to another Component or Project, so such a " +
			"change fails on apply; apply with `-replace` instead. When `component` or `project` changes because " +
			"the Component or Project was renamed or moved, only the state changes.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The numeric ID of the Add-on.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"component": schema.StringAttribute{
				MarkdownDescription: "`id` of the Component that holds the Add-on, which is its slug path, such as " +
					"`docs/guides/app`. Exactly one of `component` and `project` must be set.",
				Optional: true,
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "Slug of the Project that holds the Add-on. Exactly one of `component` and " +
					"`project` must be set.",
				Optional: true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the Add-on in Weblate, such as `weblate.gettext.mo`.",
				Required:            true,
			},
			"configuration": schema.StringAttribute{
				MarkdownDescription: "Settings of the Add-on as a JSON object, set with `jsonencode`. Weblate " +
					"replaces the whole configuration, so a key that is left out is reset to the Add-on's default. " +
					"Without it, the provider reads the configuration but never changes it.",
				CustomType:    jsontypes.NormalizedType{},
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *addonResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*client.Client); ok {
		r.client = c
	}
}

func (r *addonResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m addonResourceModel
	if resp.Diagnostics.Append(req.Config.Get(ctx, &m)...); resp.Diagnostics.HasError() {
		return
	}
	if m.Component.IsUnknown() || m.Project.IsUnknown() || m.Component.IsNull() != m.Project.IsNull() {
		return
	}
	resp.Diagnostics.AddAttributeError(path.Root("component"), "Component or Project needed",
		"An Add-on is on a Component or on a Project. Set exactly one of component and project.")
}

// owner returns the API path of the Component or Project that holds the Add-on of m.
func (m addonResourceModel) owner() string {
	if !m.Component.IsNull() {
		return client.ComponentRef(m.Component.ValueString())
	}
	return client.ProjectRef(m.Project.ValueString())
}

func (r *addonResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	fields, diags := fieldsToWrite(ctx, req.Config, req.Plan, nil)
	resp.Diagnostics.Append(diags...)
	var plan addonResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The Component or Project is in the URL; Weblate's serializer has them read-only.
	delete(fields, "component")
	delete(fields, "project")
	a, err := r.client.CreateAddon(ctx, plan.owner(), fields)
	if client.IsAddonAlreadyInstalled(err, plan.Name.ValueString()) {
		a, err = r.adopt(ctx, plan, fields, err)
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Installing the Add-on failed", err))
		return
	}
	storeWrittenAddon(ctx, req.Config, a, &resp.State, &resp.Diagnostics)
}

// adopt finds the installed Add-on that Weblate refused to install again and sends the configured configuration to
// it (ADR 0007). installErr is returned when Weblate does not list the Add-on.
func (r *addonResource) adopt(ctx context.Context, plan addonResourceModel, fields map[string]any, installErr error) (*client.Addon, error) {
	a, err := r.client.FindAddon(ctx, plan.owner(), plan.Name.ValueString())
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, installErr
	}
	if configuration, ok := fields["configuration"]; ok {
		return r.client.UpdateAddon(ctx, a.ID, map[string]any{"configuration": configuration})
	}
	return a, nil
}

func (r *addonResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	id, diags := addonID(ctx, req.State)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.GetAddon(ctx, id)
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Reading the Add-on failed", err))
		return
	}
	m, diags := newAddonResourceModel(a)
	resp.Diagnostics.Append(diags...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
}

func (r *addonResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	fields, diags := fieldsToWrite(ctx, req.Config, req.Plan, &req.State)
	resp.Diagnostics.Append(diags...)
	id, diags := addonID(ctx, req.State)
	resp.Diagnostics.Append(diags...)
	var plan addonResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, ok := fields["name"]; ok {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Add-on cannot change its name",
			fmt.Sprintf("Weblate cannot change the name of the Add-on %d. To remove it and install the Add-on %s, "+
				"apply with -replace for this resource.", id, plan.Name.ValueString()))
		return
	}
	_, componentChanged := fields["component"]
	_, projectChanged := fields["project"]
	delete(fields, "component")
	delete(fields, "project")
	var a *client.Addon
	var err error
	if componentChanged || projectChanged {
		// A planned change can also be a rename or move of the Component or Project, which Weblate already shows
		// in the Add-on.
		if a, err = r.client.GetAddon(ctx, id); err != nil {
			resp.Diagnostics.Append(apiErrorDiagnostic("Reading the Add-on failed", err))
			return
		}
		current, diags := newAddonResourceModel(a)
		if resp.Diagnostics.Append(diags...); resp.Diagnostics.HasError() {
			return
		}
		if !current.Component.Equal(plan.Component) || !current.Project.Equal(plan.Project) {
			resp.Diagnostics.AddError("Add-on cannot move",
				fmt.Sprintf("The Add-on %d is on %s, and Weblate cannot move an Add-on to another Component or "+
					"Project. To remove it there and install it on %s, apply with -replace for this resource.",
					id, current.ownerName(), plan.ownerName()))
			return
		}
	}
	switch {
	case len(fields) > 0:
		a, err = r.client.UpdateAddon(ctx, id, fields)
	case a == nil:
		a, err = r.client.GetAddon(ctx, id)
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Updating the Add-on failed", err))
		return
	}
	storeWrittenAddon(ctx, req.Config, a, &resp.State, &resp.Diagnostics)
}

func (m addonResourceModel) ownerName() string {
	if !m.Component.IsNull() {
		return "the Component " + m.Component.ValueString()
	}
	return "the Project " + m.Project.ValueString()
}

// storeWrittenAddon puts the Add-on that Weblate returned for a create or update into state, also when the read-back
// check fails, because the write happened.
func storeWrittenAddon(ctx context.Context, config tfsdk.Config, a *client.Addon, state *tfsdk.State, diags *diag.Diagnostics) {
	m, d := newAddonResourceModel(a)
	if diags.Append(d...); diags.HasError() {
		return
	}
	if diags.Append(state.Set(ctx, m)...); !diags.HasError() {
		diags.Append(readBackDiagnostics(ctx, config, *state)...)
	}
}

func (r *addonResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	id, diags := addonID(ctx, req.State)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteAddon(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.Append(apiErrorDiagnostic("Removing the Add-on failed", err))
	}
}

func (r *addonResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := strconv.ParseInt(req.ID, 10, 64); err != nil {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("The import ID of a weblate_addon is the numeric ID of the Add-on, such as 12, not %q.", req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func addonID(ctx context.Context, state tfsdk.State) (int64, diag.Diagnostics) {
	var id types.String
	diags := state.GetAttribute(ctx, path.Root("id"), &id)
	if diags.HasError() {
		return 0, diags
	}
	n, err := strconv.ParseInt(id.ValueString(), 10, 64)
	if err != nil {
		diags.AddAttributeError(path.Root("id"), "Invalid Add-on ID", err.Error())
	}
	return n, diags
}
