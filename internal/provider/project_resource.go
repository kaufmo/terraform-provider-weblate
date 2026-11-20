package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

const defaultTimeout = 20 * time.Minute

var (
	_ resource.ResourceWithConfigure   = &projectResource{}
	_ resource.ResourceWithImportState = &projectResource{}
	_ resource.ResourceWithModifyPlan  = &projectResource{}
)

type projectResource struct {
	client *client.Client
}

type projectResourceModel struct {
	ID                    types.String   `tfsdk:"id"`
	WeblateID             types.Int64    `tfsdk:"weblate_id"`
	Slug                  trimmedString  `tfsdk:"slug"`
	Name                  trimmedString  `tfsdk:"name"`
	Web                   trimmedString  `tfsdk:"web"`
	Instructions          trimmedString  `tfsdk:"instructions"`
	Workspace             trimmedString  `tfsdk:"workspace"`
	CheckFlags            trimmedString  `tfsdk:"check_flags"`
	TranslationReview     types.Bool     `tfsdk:"translation_review"`
	SourceReview          types.Bool     `tfsdk:"source_review"`
	CommitPolicy          types.Int64    `tfsdk:"commit_policy"`
	EnableHooks           types.Bool     `tfsdk:"enable_hooks"`
	LanguageAliases       trimmedString  `tfsdk:"language_aliases"`
	AccessControl         types.Int64    `tfsdk:"access_control"`
	PublicSharing         types.Bool     `tfsdk:"public_sharing"`
	UseSharedTM           types.Bool     `tfsdk:"use_shared_tm"`
	ContributeSharedTM    types.Bool     `tfsdk:"contribute_shared_tm"`
	UseWorkspaceTM        types.Bool     `tfsdk:"use_workspace_tm"`
	ContributeWorkspaceTM types.Bool     `tfsdk:"contribute_workspace_tm"`
	AutocleanTM           types.Bool     `tfsdk:"autoclean_tm"`
	Enforced2FA           types.Bool     `tfsdk:"enforced_2fa"`
	Timeouts              timeouts.Value `tfsdk:"timeouts"`
}

func newProjectResourceModel(p *client.Project, t timeouts.Value) projectResourceModel {
	return projectResourceModel{
		ID:                    types.StringValue(p.Slug),
		WeblateID:             types.Int64Value(p.ID),
		Slug:                  trimmedStringValue(p.Slug),
		Name:                  trimmedStringValue(p.Name),
		Web:                   trimmedStringValue(p.Web),
		Instructions:          trimmedStringValue(p.Instructions),
		Workspace:             trimmedStringPointerValue(p.Workspace),
		CheckFlags:            trimmedStringValue(p.CheckFlags),
		TranslationReview:     types.BoolValue(p.TranslationReview),
		SourceReview:          types.BoolValue(p.SourceReview),
		CommitPolicy:          types.Int64Value(p.CommitPolicy),
		EnableHooks:           types.BoolValue(p.EnableHooks),
		LanguageAliases:       trimmedStringValue(p.LanguageAliases),
		AccessControl:         types.Int64Value(p.AccessControl),
		PublicSharing:         types.BoolValue(p.PublicSharing),
		UseSharedTM:           types.BoolValue(p.UseSharedTM),
		ContributeSharedTM:    types.BoolValue(p.ContributeSharedTM),
		UseWorkspaceTM:        types.BoolValue(p.UseWorkspaceTM),
		ContributeWorkspaceTM: types.BoolValue(p.ContributeWorkspaceTM),
		AutocleanTM:           types.BoolValue(p.AutocleanTM),
		Enforced2FA:           types.BoolValue(p.Enforced2FA),
		Timeouts:              t,
	}
}

// NewProjectResource returns the weblate_project resource.
func NewProjectResource() resource.Resource {
	return &projectResource{}
}

func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *projectResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// Every Optional attribute has a default on the Weblate side, so it is also Computed: omitted means not managed.
	str := func(description string) schema.Attribute {
		return schema.StringAttribute{
			MarkdownDescription: description, CustomType: trimmedStringType{}, Optional: true, Computed: true,
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}
	number := func(description string) schema.Attribute {
		return schema.Int64Attribute{
			MarkdownDescription: description, Optional: true, Computed: true,
			PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
		}
	}
	boolean := func(description string) schema.Attribute {
		return schema.BoolAttribute{
			MarkdownDescription: description, Optional: true, Computed: true,
			PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Weblate Project.\n\n" +
			"Weblate strips leading and trailing whitespace from every string, so a value that differs from the " +
			"stored one only in that whitespace, such as a heredoc that ends in a newline, causes no change. " +
			"An attribute that the configuration omits is not managed: the provider reads it but never changes it, " +
			"and removing an attribute from the configuration leaves its last value in Weblate. After create and " +
			"update, the provider compares every configured attribute with the value Weblate stored and fails the " +
			"apply when they differ.\n\n" +
			"Changing `name` or `slug` renames the Project in place. Delete waits until Weblate has removed the " +
			"Project in the background.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The slug of the Project.",
				Computed:            true,
			},
			"weblate_id": schema.Int64Attribute{
				MarkdownDescription: "Numeric ID that Weblate gave the Project. It stays the same when the Project " +
					"is renamed, which lets the provider find a Project that was renamed outside OpenTofu.",
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name of the Project. Unique in the Weblate instance.",
				CustomType:          trimmedStringType{},
				Required:            true,
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the Project, as in its URL. Unique in the Weblate instance.",
				CustomType:          trimmedStringType{},
				Required:            true,
			},
			"web": str("URL of the project website. Most Weblate instances require it; " +
				"`WEBSITE_REQUIRED = False` in the Weblate settings makes it optional."),
			"instructions": str("Instructions for translators."),
			"workspace":    str("UUID of the Workspace the Project belongs to."),
			"check_flags":  str("Translation flags that customize the quality checks of the whole Project."),
			"translation_review": boolean("Whether translations need a review by a reviewer before they are " +
				"approved."),
			"source_review": boolean("Whether source strings need a review."),
			"commit_policy": number("Which translations Weblate commits: `0` all, `20` all except those that " +
				"need editing, `30` only approved ones."),
			"enable_hooks":     boolean("Whether repository notification hooks can update the Project."),
			"language_aliases": str("Language code mappings, such as `zh_CN:zh-Hans,en_US:en`."),
			"access_control": number("Access control mode: `0` public, `1` protected, `100` private, " +
				"`200` custom. Weblate lets only superusers create a Project with a mode other than the default " +
				"of the instance."),
			"public_sharing":          boolean("Whether the Project can be shared publicly."),
			"use_shared_tm":           boolean("Whether the Project uses the shared translation memory."),
			"contribute_shared_tm":    boolean("Whether the Project contributes to the shared translation memory."),
			"use_workspace_tm":        boolean("Whether the Project uses the translation memory of its Workspace."),
			"contribute_workspace_tm": boolean("Whether the Project contributes to the translation memory of its Workspace."),
			"autoclean_tm":            boolean("Whether Weblate removes outdated entries from the Project's translation memory."),
			"enforced_2fa":            boolean("Whether contributors need two-factor authentication."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{
				Delete: true,
				DeleteDescription: "How long to wait for Weblate to delete the Project in the background, such as " +
					"`30m`. Defaults to `20m`. When the time is up, the deletion goes on in Weblate.",
			}),
		},
	}
}

func (r *projectResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	// ProviderData is nil until the provider is configured.
	if c, ok := req.ProviderData.(*client.Client); ok {
		r.client = c
	}
}

// ModifyPlan plans id as the new slug, so a rename shows the new id instead of "known after apply".
func (r *projectResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var slug trimmedString
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("slug"), &slug)...)
	if slug.IsUnknown() || slug.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("id"), strings.TrimSpace(slug.ValueString()))...)
}

func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	fields, diags := fieldsToWrite(ctx, req.Config, req.Plan, nil)
	resp.Diagnostics.Append(diags...)
	var t timeouts.Value
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("timeouts"), &t)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.client.CreateProject(ctx, fields)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Creating the Project failed", err))
		return
	}
	r.storeWritten(ctx, req.Config, p, t, &resp.State, &resp.Diagnostics)
}

func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.client.GetProject(ctx, state.ID.ValueString())
	if client.IsNotFound(err) && !state.WeblateID.IsNull() {
		p, err = r.client.FindProjectByID(ctx, state.WeblateID.ValueInt64())
	} else if client.IsNotFound(err) {
		p, err = nil, nil
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Reading the Project failed", err))
		return
	}
	if p == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, newProjectResourceModel(p, state.Timeouts))...)
}

func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	fields, diags := fieldsToWrite(ctx, req.Config, req.Plan, &req.State)
	resp.Diagnostics.Append(diags...)
	var slug types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &slug)...)
	var t timeouts.Value
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("timeouts"), &t)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var p *client.Project
	var err error
	if len(fields) == 0 {
		// Only the timeouts changed; a PATCH would still run Weblate's save logic.
		p, err = r.client.GetProject(ctx, slug.ValueString())
	} else {
		p, err = r.client.UpdateProject(ctx, slug.ValueString(), fields)
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Updating the Project failed", err))
		return
	}
	r.storeWritten(ctx, req.Config, p, t, &resp.State, &resp.Diagnostics)
}

// storeWritten puts the Project that Weblate returned for a create or update into state, also when the read-back
// check fails, because the write happened.
func (r *projectResource) storeWritten(ctx context.Context, config tfsdk.Config, p *client.Project, t timeouts.Value,
	state *tfsdk.State, diags *diag.Diagnostics) {
	diags.Append(state.Set(ctx, newProjectResourceModel(p, t))...)
	if !diags.HasError() {
		diags.Append(readBackDiagnostics(ctx, config, *state)...)
	}
}

func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Delete(ctx, defaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	slug := state.ID.ValueString()
	taskURL, err := r.client.DeleteProject(ctx, slug)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Deleting the Project failed", err))
		return
	}
	if err := r.client.WaitForDeletion(ctx, client.ProjectRef(slug), taskURL); err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Deleting the Project %s failed", slug), err))
	}
}

func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
