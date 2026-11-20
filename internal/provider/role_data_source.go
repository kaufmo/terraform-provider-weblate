package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSourceWithConfigure = &roleDataSource{}

type roleDataSource struct {
	dataSourceClient
}

type roleDataSourceModel struct {
	ID          types.Int64  `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Permissions []string     `tfsdk:"permissions"`
}

func NewRoleDataSource() datasource.DataSource {
	return &roleDataSource{}
}

func (d *roleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (d *roleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a Weblate Role by its name, such as one of the built-in Roles like `Administration`.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the Role. Role names are unique.",
				Required:            true,
			},
			"id": schema.Int64Attribute{
				MarkdownDescription: "Numeric ID of the Role.",
				Computed:            true,
			},
			"permissions": schema.SetAttribute{
				MarkdownDescription: "Codes of the permissions the Role grants, such as `project.edit`.",
				ElementType:         types.StringType,
				Computed:            true,
			},
		},
	}
}

func (d *roleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var name types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("name"), &name)...)
	if resp.Diagnostics.HasError() {
		return
	}
	roles, err := d.client.ListRoles(ctx)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Reading the Roles failed", err))
		return
	}
	for _, r := range roles {
		if r.Name == name.ValueString() {
			resp.Diagnostics.Append(resp.State.Set(ctx, roleDataSourceModel{
				ID:          types.Int64Value(r.ID),
				Name:        types.StringValue(r.Name),
				Permissions: r.Permissions,
			})...)
			return
		}
	}
	resp.Diagnostics.AddAttributeError(path.Root("name"), "Role not found",
		fmt.Sprintf("Weblate has no Role named %q, or the API token cannot see it. Without the role.view "+
			"permission, the token sees only the Roles of its own Groups.", name.ValueString()))
}
