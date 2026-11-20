// Package provider implements the weblate provider with its resources and data sources.
package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

var _ provider.Provider = &weblateProvider{}

type weblateProvider struct {
	version string
}

type providerModel struct {
	URL   types.String `tfsdk:"url"`
	Token types.String `tfsdk:"token"`
}

// New returns the provider factory for the given release version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &weblateProvider{version: version}
	}
}

func (p *weblateProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "weblate"
	resp.Version = p.version
}

func (p *weblateProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the configuration of a Weblate instance through its REST API.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				MarkdownDescription: "Base URL of the Weblate instance, such as `https://weblate.example.com`, or " +
					"`https://example.com/weblate` for an instance served under `WEBLATE_URL_PREFIX`. " +
					"The provider appends `/api/` itself, so the URL must not end in `/api`. " +
					"Defaults to the `WEBLATE_URL` environment variable.",
				Optional: true,
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "API token of a Weblate user (`wlu_…`) or Project (`wlp_…`). " +
					"Defaults to the `WEBLATE_API_TOKEN` environment variable.",
				Optional:  true,
				Sensitive: true,
			},
		},
	}
}

func (p *weblateProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	url := valueOrEnv(&resp.Diagnostics, cfg.URL, "url", "WEBLATE_URL")
	token := valueOrEnv(&resp.Diagnostics, cfg.Token, "token", "WEBLATE_API_TOKEN")
	if resp.Diagnostics.HasError() {
		return
	}

	c, err := client.New(url, token)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("url"), "Invalid Weblate URL", err.Error())
		return
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

// valueOrEnv returns the value from the provider block, or from the environment variable env when the block leaves
// the attribute out.
func valueOrEnv(diags *diag.Diagnostics, v types.String, attribute, env string) string {
	if v.IsUnknown() {
		diags.AddAttributeError(path.Root(attribute), "Unknown "+attribute,
			fmt.Sprintf("The provider needs %s while it plans. Set it to a value that is known before apply, or "+
				"leave it out and set the %s environment variable.", attribute, env))
		return ""
	}
	value := v.ValueString()
	if v.IsNull() {
		value = os.Getenv(env)
	}
	if value == "" {
		diags.AddAttributeError(path.Root(attribute), "Missing "+attribute,
			fmt.Sprintf("Set %s in the provider block or the %s environment variable.", attribute, env))
	}
	return value
}

func (p *weblateProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewProjectResource}
}

func (p *weblateProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{NewProjectDataSource}
}
