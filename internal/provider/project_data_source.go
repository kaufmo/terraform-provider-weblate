package provider

import (
	"context"
	"fmt"
	"maps"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

var _ datasource.DataSourceWithConfigure = &projectDataSource{}

type projectDataSource struct {
	client *client.Client
}

type projectDataSourceModel struct {
	ID                    types.String `tfsdk:"id"`
	WeblateID             types.Int64  `tfsdk:"weblate_id"`
	Slug                  types.String `tfsdk:"slug"`
	Name                  types.String `tfsdk:"name"`
	Web                   types.String `tfsdk:"web"`
	Instructions          types.String `tfsdk:"instructions"`
	Workspace             types.String `tfsdk:"workspace"`
	CheckFlags            types.String `tfsdk:"check_flags"`
	TranslationReview     types.Bool   `tfsdk:"translation_review"`
	SourceReview          types.Bool   `tfsdk:"source_review"`
	CommitPolicy          types.Int64  `tfsdk:"commit_policy"`
	EnableHooks           types.Bool   `tfsdk:"enable_hooks"`
	LanguageAliases       types.String `tfsdk:"language_aliases"`
	AccessControl         types.Int64  `tfsdk:"access_control"`
	PublicSharing         types.Bool   `tfsdk:"public_sharing"`
	UseSharedTM           types.Bool   `tfsdk:"use_shared_tm"`
	ContributeSharedTM    types.Bool   `tfsdk:"contribute_shared_tm"`
	UseWorkspaceTM        types.Bool   `tfsdk:"use_workspace_tm"`
	ContributeWorkspaceTM types.Bool   `tfsdk:"contribute_workspace_tm"`
	AutocleanTM           types.Bool   `tfsdk:"autoclean_tm"`
	Enforced2FA           types.Bool   `tfsdk:"enforced_2fa"`
	inheritedSettingsModel
}

func NewProjectDataSource() datasource.DataSource {
	return &projectDataSource{}
}

func (d *projectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *projectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	str := func(description string) schema.Attribute {
		return schema.StringAttribute{MarkdownDescription: description, Computed: true}
	}
	number := func(description string) schema.Attribute {
		return schema.Int64Attribute{MarkdownDescription: description, Computed: true}
	}
	boolean := func(description string) schema.Attribute {
		return schema.BoolAttribute{MarkdownDescription: description, Computed: true}
	}
	attributes := inheritedSettingDataSourceAttributes()
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a Weblate Project by its slug, for example to grant a Group access to a Project " +
			"that another configuration manages. While an Inherited setting is inherited, such as with " +
			"`inherit_license = true`, the setting holds the value that Weblate stores for the Project, which has " +
			"no effect.",
		Attributes: attributes,
	}
	maps.Copy(attributes, map[string]schema.Attribute{
		"slug": schema.StringAttribute{
			MarkdownDescription: "Slug of the Project, as in its URL.",
			Required:            true,
		},
		"id":           str("The slug of the Project."),
		"weblate_id":   number("Numeric ID that Weblate gave the Project. It stays the same when the Project is renamed."),
		"name":         str("Display name of the Project."),
		"web":          str("URL of the project website."),
		"instructions": str("Instructions for translators."),
		"workspace":    str("UUID of the Workspace the Project belongs to, or `null`."),
		"check_flags":  str("Translation flags that customize the quality checks of the whole Project."),
		"translation_review": boolean("Whether translations need a review by a reviewer before they are " +
			"approved."),
		"source_review": boolean("Whether source strings need a review."),
		"commit_policy": number("Which translations Weblate commits: `0` all, `20` all except those that " +
			"need editing, `30` only approved ones."),
		"enable_hooks":     boolean("Whether repository notification hooks can update the Project."),
		"language_aliases": str("Language code mappings, such as `zh_CN:zh-Hans,en_US:en`."),
		"access_control": number("Access control mode: `0` public, `1` protected, `100` private, " +
			"`200` custom."),
		"public_sharing":          boolean("Whether the Project can be shared publicly."),
		"use_shared_tm":           boolean("Whether the Project uses the shared translation memory."),
		"contribute_shared_tm":    boolean("Whether the Project contributes to the shared translation memory."),
		"use_workspace_tm":        boolean("Whether the Project uses the translation memory of its Workspace."),
		"contribute_workspace_tm": boolean("Whether the Project contributes to the translation memory of its Workspace."),
		"autoclean_tm":            boolean("Whether Weblate removes outdated entries from the Project's translation memory."),
		"enforced_2fa":            boolean("Whether contributors need two-factor authentication."),
	})
}

func (d *projectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	// ProviderData is nil until the provider is configured.
	if c, ok := req.ProviderData.(*client.Client); ok {
		d.client = c
	}
}

func (d *projectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var slug types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("slug"), &slug)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := d.client.GetProject(ctx, slug.ValueString())
	if client.IsNotFound(err) {
		resp.Diagnostics.AddAttributeError(path.Root("slug"), "Project not found",
			fmt.Sprintf("Weblate has no Project with the slug %q, or the API token cannot see it.", slug.ValueString()))
		return
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Reading the Project failed", err))
		return
	}
	inherited, diags := newInheritedSettingsModel(ctx, p.Fields)
	if resp.Diagnostics.Append(diags...); resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectDataSourceModel{
		ID:                     types.StringValue(p.Slug),
		WeblateID:              types.Int64Value(p.ID),
		Slug:                   types.StringValue(p.Slug),
		Name:                   types.StringValue(p.Name),
		Web:                    types.StringValue(p.Web),
		Instructions:           types.StringValue(p.Instructions),
		Workspace:              types.StringPointerValue(p.Workspace),
		CheckFlags:             types.StringValue(p.CheckFlags),
		TranslationReview:      types.BoolValue(p.TranslationReview),
		SourceReview:           types.BoolValue(p.SourceReview),
		CommitPolicy:           types.Int64Value(p.CommitPolicy),
		EnableHooks:            types.BoolValue(p.EnableHooks),
		LanguageAliases:        types.StringValue(p.LanguageAliases),
		AccessControl:          types.Int64Value(p.AccessControl),
		PublicSharing:          types.BoolValue(p.PublicSharing),
		UseSharedTM:            types.BoolValue(p.UseSharedTM),
		ContributeSharedTM:     types.BoolValue(p.ContributeSharedTM),
		UseWorkspaceTM:         types.BoolValue(p.UseWorkspaceTM),
		ContributeWorkspaceTM:  types.BoolValue(p.ContributeWorkspaceTM),
		AutocleanTM:            types.BoolValue(p.AutocleanTM),
		Enforced2FA:            types.BoolValue(p.Enforced2FA),
		inheritedSettingsModel: inherited,
	})...)
}
