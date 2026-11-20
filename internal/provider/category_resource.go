package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &categoryResource{}
	_ resource.ResourceWithImportState = &categoryResource{}
	_ resource.ResourceWithModifyPlan  = &categoryResource{}
)

type categoryResource struct {
	client *client.Client
}

type categoryResourceModel struct {
	ID        types.String   `tfsdk:"id"`
	WeblateID types.Int64    `tfsdk:"weblate_id"`
	Project   types.String   `tfsdk:"project"`
	Category  types.String   `tfsdk:"category"`
	Name      trimmedString  `tfsdk:"name"`
	Slug      trimmedString  `tfsdk:"slug"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
}

func newCategoryResourceModel(c *client.Category, slugPath string, t timeouts.Value) categoryResourceModel {
	parent := types.StringNull()
	if c.Category != nil {
		parent = types.StringValue(slugPath[:strings.LastIndex(slugPath, "/")])
	}
	return categoryResourceModel{
		ID:        types.StringValue(slugPath),
		WeblateID: types.Int64Value(c.ID),
		Project:   types.StringValue(c.ProjectSlug()),
		Category:  parent,
		Name:      trimmedStringValue(c.Name),
		Slug:      trimmedStringValue(c.Slug),
		Timeouts:  t,
	}
}

// NewCategoryResource returns the weblate_category resource.
func NewCategoryResource() resource.Resource {
	return &categoryResource{}
}

func (r *categoryResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_category"
}

func (r *categoryResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Weblate Category, a folder inside a Project that groups Components. " +
			"Categories can nest up to three levels deep.\n\n" +
			"Changing `name` or `slug` renames the Category in place, and changing `category` moves it in place. " +
			"Weblate cannot move a Category to another Project, so changing `project` to another Project fails on " +
			"apply. When `project` changes because the Project was renamed, only the state changes. Delete waits " +
			"until Weblate has removed the Category in the background.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The slug path of the Category: the Project slug and the slugs of all " +
					"Categories down to this one, joined by `/`, such as `docs/guides`.",
				Computed: true,
			},
			"weblate_id": schema.Int64Attribute{
				MarkdownDescription: "Numeric ID that Weblate gave the Category. It stays the same when the " +
					"Category is renamed or moved.",
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "Slug of the Project that holds the Category.",
				Required:            true,
			},
			"category": schema.StringAttribute{
				MarkdownDescription: "`id` of the parent Category, which is its slug path and must start with " +
					"`project`. Without it, the Category is at the root of the Project.",
				Optional: true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name of the Category. Unique among the Categories with the same parent.",
				CustomType:          trimmedStringType{},
				Required:            true,
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the Category, as in its URL. Unique among the Categories with the " +
					"same parent.",
				CustomType: trimmedStringType{},
				Required:   true,
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{
				Delete: true,
				DeleteDescription: "How long to wait for Weblate to delete the Category in the background, such as " +
					"`30m`. Defaults to `20m`. When the time is up, the deletion goes on in Weblate.",
			}),
		},
	}
}

func (r *categoryResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*client.Client); ok {
		r.client = c
	}
}

// ModifyPlan rejects a parent Category outside the Project and plans id as the new slug path.
func (r *categoryResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan categoryResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	parent := plan.Category
	if parent.IsNull() {
		parent = plan.Project
	} else if !plan.Project.IsUnknown() && !parent.IsUnknown() &&
		!strings.HasPrefix(parent.ValueString(), plan.Project.ValueString()+"/") {
		resp.Diagnostics.AddAttributeError(path.Root("category"), "Parent Category outside the Project",
			fmt.Sprintf("The parent Category %s is not in the Project %s. Set category to the id of a Category "+
				"in that Project.", parent.ValueString(), plan.Project.ValueString()))
		return
	}
	if parent.IsUnknown() || plan.Slug.IsUnknown() {
		return
	}
	id := parent.ValueString() + "/" + strings.TrimSpace(plan.Slug.ValueString())
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("id"), id)...)
}

func (r *categoryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	fields, diags := fieldsToWrite(ctx, req.Config, req.Plan, nil)
	resp.Diagnostics.Append(diags...)
	var plan categoryResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	fields["project"] = r.client.ProjectURL(plan.Project.ValueString())
	if !plan.Category.IsNull() {
		if fields["category"] = r.parentURL(ctx, plan.Category, &resp.Diagnostics); resp.Diagnostics.HasError() {
			return
		}
	}
	c, err := r.client.CreateCategory(ctx, fields)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Creating the Category failed", err))
		return
	}
	r.store(ctx, c, plan.Timeouts, &resp.State, &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(readBackDiagnostics(ctx, req.Config, resp.State)...)
	}
}

// parentURL returns the API URL of the Category with the given slug path, the form in which Weblate takes a parent.
func (r *categoryResource) parentURL(ctx context.Context, slugPath types.String, diags *diag.Diagnostics) any {
	parent, err := r.client.FindCategory(ctx, slugPath.ValueString())
	if err != nil {
		diags.Append(apiErrorDiagnostic("Finding the parent Category failed", err))
		return nil
	}
	if parent == nil {
		diags.AddAttributeError(path.Root("category"), "Parent Category not found",
			fmt.Sprintf("Weblate has no Category with the slug path %s.", slugPath.ValueString()))
		return nil
	}
	return parent.URL
}

func (r *categoryResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state categoryResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := r.client.GetCategory(ctx, state.WeblateID.ValueInt64())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Reading the Category failed", err))
		return
	}
	r.store(ctx, c, state.Timeouts, &resp.State, &resp.Diagnostics)
}

func (r *categoryResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	fields, diags := fieldsToWrite(ctx, req.Config, req.Plan, &req.State)
	resp.Diagnostics.Append(diags...)
	var plan, state categoryResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Weblate ignores project on update; a parent is sent below as an API URL, and a move to the root as null.
	delete(fields, "project")
	delete(fields, "category")
	id := state.WeblateID.ValueInt64()

	var c *client.Category
	var err error
	if !plan.Project.Equal(state.Project) {
		if c, err = r.client.GetCategory(ctx, id); err != nil {
			resp.Diagnostics.Append(apiErrorDiagnostic("Reading the Category failed", err))
			return
		}
		// Slugs are unique, so the same slug means the same Project, renamed since the last refresh.
		if c.ProjectSlug() != plan.Project.ValueString() {
			resp.Diagnostics.AddAttributeError(path.Root("project"), "Category cannot change its Project",
				fmt.Sprintf("The Category %s is in the Project %s, and Weblate cannot move a Category to another "+
					"Project. To delete it there and create it in the Project %s, which deletes the Components "+
					"inside it, apply with -replace for this resource.",
					state.ID.ValueString(), c.ProjectSlug(), plan.Project.ValueString()))
			return
		}
	}
	if !plan.Category.Equal(state.Category) {
		fields["category"] = nil
		if !plan.Category.IsNull() {
			if fields["category"] = r.parentURL(ctx, plan.Category, &resp.Diagnostics); resp.Diagnostics.HasError() {
				return
			}
		}
	}
	switch {
	case len(fields) > 0:
		c, err = r.client.UpdateCategory(ctx, id, fields)
	case c == nil:
		c, err = r.client.GetCategory(ctx, id)
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Updating the Category failed", err))
		return
	}
	r.store(ctx, c, plan.Timeouts, &resp.State, &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(readBackDiagnostics(ctx, req.Config, resp.State)...)
	}
}

// store puts the Category into state, with its slug path read from Weblate.
func (r *categoryResource) store(ctx context.Context, c *client.Category, t timeouts.Value, state *tfsdk.State,
	diags *diag.Diagnostics) {
	slugPath, err := r.client.CategorySlugPath(ctx, c)
	if err != nil {
		diags.Append(apiErrorDiagnostic("Reading the parent Categories failed", err))
		return
	}
	diags.Append(state.Set(ctx, newCategoryResourceModel(c, slugPath, t))...)
}

func (r *categoryResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state categoryResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Delete(ctx, defaultDeleteTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id := state.WeblateID.ValueInt64()
	err := r.client.DeleteCategory(ctx, id)
	if client.IsNotFound(err) {
		return
	}
	if err == nil {
		err = r.client.WaitForDeletion(ctx, client.CategoryRef(id), "")
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Deleting the Category %s failed", state.ID.ValueString()), err))
	}
}

// ImportState takes the slug path and finds the Category's numeric ID, by which Read reads it.
func (r *categoryResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	c, err := r.client.FindCategory(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Finding the Category failed", err))
		return
	}
	if c == nil {
		resp.Diagnostics.AddError("Category not found", fmt.Sprintf("Weblate has no Category with the slug path "+
			"%s. The import ID is the slug path, such as docs/guides.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("weblate_id"), c.ID)...)
}
