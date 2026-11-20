package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

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
	ID                         types.String         `tfsdk:"id"`
	Component                  types.String         `tfsdk:"component"`
	Project                    types.String         `tfsdk:"project"`
	Name                       types.String         `tfsdk:"name"`
	Configuration              jsontypes.Normalized `tfsdk:"configuration"`
	SecretConfiguration        jsontypes.Normalized `tfsdk:"secret_configuration_wo"`
	SecretConfigurationVersion types.Int64          `tfsdk:"secret_configuration_wo_version"`
}

// secretKeysPrivateKey is the private state key of the key names of secret_configuration_wo at the last write that
// sent the configuration. Read has no access to the configuration but has to remove these keys from what Weblate
// returns (ADR 0005).
const secretKeysPrivateKey = "secret_configuration_keys"

// knownSecretKeys are the configuration keys that hold a secret, by Add-on name, so that an imported Add-on does not
// write them into state either.
var knownSecretKeys = map[string][]string{"weblate.webhook.webhook": {"secret"}}

// removeSecrets removes the given keys and the known secret keys of the Add-on from the configuration of a.
func removeSecrets(a *client.Addon, secretKeys []string) {
	var configuration map[string]json.RawMessage
	if json.Unmarshal(a.Configuration, &configuration) != nil || configuration == nil {
		return
	}
	for _, key := range slices.Concat(secretKeys, knownSecretKeys[a.Name]) {
		delete(configuration, key)
	}
	a.Configuration, _ = json.Marshal(configuration)
}

type privateState interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

func storedSecretKeys(ctx context.Context, private privateState) ([]string, diag.Diagnostics) {
	data, diags := private.GetKey(ctx, secretKeysPrivateKey)
	var keys []string
	if len(data) > 0 {
		if err := json.Unmarshal(data, &keys); err != nil {
			diags.AddError("Invalid private state", fmt.Sprintf("The provider stored %s as the secret configuration "+
				"keys: %s. This is a bug in the provider.", data, err))
		}
	}
	return keys, diags
}

func storeSecretKeys(ctx context.Context, private privateState, keys []string) diag.Diagnostics {
	data, _ := json.Marshal(keys)
	return private.SetKey(ctx, secretKeysPrivateKey, data)
}

// addSecretConfiguration adds the keys of secret_configuration_wo from config to the configuration in fields, because
// a PATCH replaces the whole configuration, and returns their names. sent is false when fields has no configuration.
func addSecretConfiguration(ctx context.Context, config tfsdk.Config, fields map[string]any) (secret jsontypes.Normalized, keys []string, sent bool, diags diag.Diagnostics) {
	delete(fields, "secret_configuration_wo_version")
	configuration, sent := fields["configuration"].(map[string]any)
	diags = config.GetAttribute(ctx, path.Root("secret_configuration_wo"), &secret)
	if !sent || secret.IsNull() || diags.HasError() {
		return jsontypes.NewNormalizedNull(), nil, sent, diags
	}
	var secrets map[string]any
	diags.Append(secret.Unmarshal(&secrets)...)
	maps.Copy(configuration, secrets)
	return secret, slices.Sorted(maps.Keys(secrets)), sent, diags
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
	configuration, err := serializerField{kind: jsonField}.value(a.Configuration)
	if err != nil {
		diags.AddAttributeError(path.Root("configuration"), "Unexpected value from Weblate",
			fmt.Sprintf("Weblate returned %s for configuration: %s.", a.Configuration, err))
	}
	m.Configuration = configuration.(jsontypes.Normalized)
	return m, diags
}

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
			"secret_configuration_wo": schema.StringAttribute{
				MarkdownDescription: "Secret settings of the Add-on as a JSON object, set with `jsonencode`, such as " +
					"the `secret` of the `weblate.webhook.webhook` Add-on. Needs `configuration`; a key in both is " +
					"an error. The provider adds these keys to `configuration` on every write that sends " +
					"`configuration` and never stores them in plan or state. A changed secret shows in the plan " +
					"only when `secret_configuration_wo_version` changes, and the provider cannot detect a secret " +
					"that was changed outside OpenTofu.",
				CustomType: jsontypes.NormalizedType{},
				Optional:   true,
				WriteOnly:  true,
			},
			"secret_configuration_wo_version": schema.Int64Attribute{
				MarkdownDescription: "Change this number to send `secret_configuration_wo` again, such as after " +
					"changing a secret.",
				Optional: true,
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
	if !m.Component.IsUnknown() && !m.Project.IsUnknown() && m.Component.IsNull() == m.Project.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("component"), "Component or Project needed",
			"An Add-on is on a Component or on a Project. Set exactly one of component and project.")
	}
	resp.Diagnostics.Append(validateSecretConfiguration(m)...)
}

func validateSecretConfiguration(m addonResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	var secret, configuration map[string]any
	switch {
	case m.SecretConfiguration.IsNull() || m.SecretConfiguration.IsUnknown():
	case m.SecretConfiguration.Unmarshal(&secret).HasError():
		diags.AddAttributeError(path.Root("secret_configuration_wo"), "JSON object needed",
			"Set secret_configuration_wo to a JSON object with jsonencode.")
	case m.Configuration.IsNull():
		diags.AddAttributeError(path.Root("configuration"), "Configuration needed",
			"Weblate replaces the whole configuration of an Add-on, so the provider sends secret_configuration_wo "+
				"only together with configuration. Set configuration.")
	case m.Configuration.IsUnknown():
	case m.Configuration.Unmarshal(&configuration).HasError():
		diags.AddAttributeError(path.Root("configuration"), "JSON object needed",
			"With secret_configuration_wo, set configuration to a JSON object with jsonencode.")
	default:
		var both []string
		for _, key := range slices.Sorted(maps.Keys(secret)) {
			if _, ok := configuration[key]; ok {
				both = append(both, key)
			}
		}
		if len(both) > 0 {
			diags.AddAttributeError(path.Root("secret_configuration_wo"), "Key in configuration and secret configuration",
				fmt.Sprintf("The keys %s are in both configuration and secret_configuration_wo. Set each key in one "+
					"of them; only secret_configuration_wo keeps a key out of state.", strings.Join(both, ", ")))
		}
	}
	return diags
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
	secret, keys, sent, diags := addSecretConfiguration(ctx, req.Config, fields)
	if resp.Diagnostics.Append(diags...); resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.CreateAddon(ctx, plan.owner(), fields)
	if client.IsAddonAlreadyInstalled(err, plan.Name.ValueString()) {
		a, err = r.adopt(ctx, plan, fields, err)
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Installing the Add-on failed", err))
		return
	}
	if sent {
		resp.Diagnostics.Append(storeSecretKeys(ctx, resp.Private, keys)...)
	}
	storeWrittenAddon(ctx, req.Config, a, secret, keys, &resp.State, &resp.Diagnostics)
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
	keys, diags := storedSecretKeys(ctx, req.Private)
	resp.Diagnostics.Append(diags...)
	removeSecrets(a, keys)
	m, diags := newAddonResourceModel(a)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("secret_configuration_wo_version"),
		&m.SecretConfigurationVersion)...)
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
	var configured jsontypes.Normalized
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("configuration"), &configured)...)
	if _, bumped := fields["secret_configuration_wo_version"]; bumped && !configured.IsNull() {
		fields["configuration"], diags = jsonValue(ctx, path.Root("configuration"), configured)
		resp.Diagnostics.Append(diags...)
	}
	secret, keys, sent, diags := addSecretConfiguration(ctx, req.Config, fields)
	resp.Diagnostics.Append(diags...)
	if !sent {
		keys, diags = storedSecretKeys(ctx, req.Private)
		resp.Diagnostics.Append(diags...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
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
	if sent {
		resp.Diagnostics.Append(storeSecretKeys(ctx, resp.Private, keys)...)
	}
	storeWrittenAddon(ctx, req.Config, a, secret, keys, &resp.State, &resp.Diagnostics)
}

func (m addonResourceModel) ownerName() string {
	if !m.Component.IsNull() {
		return "the Component " + m.Component.ValueString()
	}
	return "the Project " + m.Project.ValueString()
}

// storeWrittenAddon puts the Add-on that Weblate returned for a create or update into state without the secret keys,
// also when the read-back check fails, because the write happened. sentSecret is the secret_configuration_wo that the
// write sent, or null.
func storeWrittenAddon(ctx context.Context, config tfsdk.Config, a *client.Addon, sentSecret jsontypes.Normalized,
	secretKeys []string, state *tfsdk.State, diags *diag.Diagnostics) {
	if changed := changedKeysSentence(sentSecret, jsontypes.NewNormalizedValue(string(a.Configuration))); changed != "" {
		diags.AddAttributeError(path.Root("secret_configuration_wo"), "Weblate stored a different value",
			"Weblate did not store secret_configuration_wo as sent; the values are secret and not shown."+changed)
	}
	removeSecrets(a, secretKeys)
	m, d := newAddonResourceModel(a)
	if diags.Append(d...); diags.HasError() {
		return
	}
	diags.Append(config.GetAttribute(ctx, path.Root("secret_configuration_wo_version"), &m.SecretConfigurationVersion)...)
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
