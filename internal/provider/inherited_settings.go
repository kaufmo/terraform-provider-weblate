package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// inheritedSettings are the Inherited settings that the serializers of Project, Category and Component share. Each
// one also has the attribute inherit_<setting> (ADR 0006).
var inheritedSettings = map[string]serializerField{
	"license": {kind: stringField, description: "License of the translations, as one of the license IDs that the " +
		"Weblate instance offers, such as `MIT`."},
	"agreement": {kind: stringField, description: "Contributor license agreement that users have to accept " +
		"before they translate."},
	"new_lang": {kind: stringField, description: "How users start a new translation: `add`, `contact`, `url`, " +
		"`existing` or `none`. `url` needs `instructions` on the Project."},
	"language_code_style": {kind: stringField, description: "Style of the language codes in file names, such as " +
		"`posix` or `bcp`. Empty means the style of the file format."},
	"secondary_language": {kind: int64Field, description: "Numeric ID of the Weblate language that Weblate shows " +
		"translators in addition to the source language."},
	"enforced_checks": {kind: stringSetField, description: "IDs of the quality checks that translators cannot " +
		"dismiss, such as `same`. The provider does not check the IDs; Weblate rejects an unknown one."},
	"commit_message": {kind: stringField, description: "Template of the commit message for a translation."},
	"add_message":    {kind: stringField, description: "Template of the commit message for a new translation."},
	"delete_message": {kind: stringField, description: "Template of the commit message for a removed translation."},
	"merge_message":  {kind: stringField, description: "Template of the commit message for a merge of the upstream repository."},
	"addon_message":  {kind: stringField, description: "Template of the commit message for a change by an Add-on."},
	"pull_message":   {kind: stringField, description: "Template of the title and description of a pull request that Weblate opens."},
}

// inheritedSettingFields are the attributes of the Inherited settings: each setting and its inherit_<setting>.
var inheritedSettingFields = func() map[string]serializerField {
	fields := maps.Clone(inheritedSettings)
	for name := range inheritedSettings {
		fields["inherit_"+name] = serializerField{kind: boolField, description: fmt.Sprintf("Whether `%[1]s` "+
			"comes from the next level up instead of this object. `true` together with `%[1]s` fails at plan. "+
			"Omitted while `%[1]s` is set, it is `false`.", name)}
	}
	return fields
}()

const inheritedSettingsDescription = "Each Inherited setting is a pair of attributes, such as `license` and " +
	"`inherit_license`. With `license` set, the provider sends `inherit_license = false` with it. With " +
	"`inherit_license = true`, the setting comes from the next level up: Category, Project, then Workspace. " +
	"Setting both fails at plan, and with both omitted the setting is not managed. While a setting is inherited, " +
	"state holds the value that Weblate stores for this object, which has no effect."

// inheritedSettingsModel holds the Inherited settings in the model of a resource or data source.
type inheritedSettingsModel struct {
	License                  trimmedString `tfsdk:"license"`
	InheritLicense           types.Bool    `tfsdk:"inherit_license"`
	Agreement                trimmedString `tfsdk:"agreement"`
	InheritAgreement         types.Bool    `tfsdk:"inherit_agreement"`
	NewLang                  trimmedString `tfsdk:"new_lang"`
	InheritNewLang           types.Bool    `tfsdk:"inherit_new_lang"`
	LanguageCodeStyle        trimmedString `tfsdk:"language_code_style"`
	InheritLanguageCodeStyle types.Bool    `tfsdk:"inherit_language_code_style"`
	SecondaryLanguage        types.Int64   `tfsdk:"secondary_language"`
	InheritSecondaryLanguage types.Bool    `tfsdk:"inherit_secondary_language"`
	EnforcedChecks           types.Set     `tfsdk:"enforced_checks"`
	InheritEnforcedChecks    types.Bool    `tfsdk:"inherit_enforced_checks"`
	CommitMessage            trimmedString `tfsdk:"commit_message"`
	InheritCommitMessage     types.Bool    `tfsdk:"inherit_commit_message"`
	AddMessage               trimmedString `tfsdk:"add_message"`
	InheritAddMessage        types.Bool    `tfsdk:"inherit_add_message"`
	DeleteMessage            trimmedString `tfsdk:"delete_message"`
	InheritDeleteMessage     types.Bool    `tfsdk:"inherit_delete_message"`
	MergeMessage             trimmedString `tfsdk:"merge_message"`
	InheritMergeMessage      types.Bool    `tfsdk:"inherit_merge_message"`
	AddonMessage             trimmedString `tfsdk:"addon_message"`
	InheritAddonMessage      types.Bool    `tfsdk:"inherit_addon_message"`
	PullMessage              trimmedString `tfsdk:"pull_message"`
	InheritPullMessage       types.Bool    `tfsdk:"inherit_pull_message"`
}

// newInheritedSettingsModel converts the Inherited settings from a Weblate response, given as the raw JSON of every
// field by name.
func newInheritedSettingsModel(ctx context.Context, raw map[string]json.RawMessage) (inheritedSettingsModel, diag.Diagnostics) {
	var m inheritedSettingsModel
	var diags diag.Diagnostics
	values := map[string]attr.Value{}
	attrTypes := map[string]attr.Type{}
	for name, f := range inheritedSettingFields {
		v, err := f.value(raw[name])
		if err != nil {
			diags.AddAttributeError(path.Root(name), "Unexpected value from Weblate",
				fmt.Sprintf("Weblate returned %s for %s: %s.", raw[name], name, err))
			return m, diags
		}
		values[name] = v
		attrTypes[name] = v.Type(ctx)
	}
	object, d := types.ObjectValue(attrTypes, values)
	diags.Append(d...)
	if !diags.HasError() {
		diags.Append(object.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	}
	return m, diags
}

func inheritedSettingAttributes() map[string]schema.Attribute {
	attributes := map[string]schema.Attribute{}
	for name, f := range inheritedSettingFields {
		attributes[name] = f.attribute()
	}
	return attributes
}

func inheritedSettingDataSourceAttributes() map[string]dsschema.Attribute {
	attributes := map[string]dsschema.Attribute{}
	for name, f := range inheritedSettingFields {
		if setting, ok := strings.CutPrefix(name, "inherit_"); ok {
			f.description = fmt.Sprintf("Whether `%s` comes from the Workspace instead of the Project.", setting)
		}
		attributes[name] = f.dataSourceAttribute()
	}
	return attributes
}

// validateInheritedSettings rejects a setting configured together with inherit_<setting> = true. Weblate clears the
// flag whenever an update changes the value, so such a configuration would never converge.
func validateInheritedSettings(ctx context.Context, config tfsdk.Config) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, name := range slices.Sorted(maps.Keys(inheritedSettings)) {
		var value attr.Value
		var inherit types.Bool
		diags.Append(config.GetAttribute(ctx, path.Root(name), &value)...)
		diags.Append(config.GetAttribute(ctx, path.Root("inherit_"+name), &inherit)...)
		if inherit.ValueBool() && value != nil && !value.IsNull() && !value.IsUnknown() {
			diags.AddAttributeError(path.Root("inherit_"+name), "Inherited setting with its own value",
				fmt.Sprintf("With inherit_%[1]s = true, %[1]s comes from the next level up and Weblate ignores "+
					"the value of %[1]s. Remove %[1]s or inherit_%[1]s.", name))
		}
	}
	return diags
}

// planInheritedSettings plans inherit_<setting> as false for every setting that config sets without the flag,
// because the provider sends that flag with the value.
func planInheritedSettings(ctx context.Context, config tfsdk.Config, plan *tfsdk.Plan) diag.Diagnostics {
	var diags diag.Diagnostics
	for name := range inheritedSettings {
		var value attr.Value
		var inherit types.Bool
		diags.Append(config.GetAttribute(ctx, path.Root(name), &value)...)
		diags.Append(config.GetAttribute(ctx, path.Root("inherit_"+name), &inherit)...)
		if diags.HasError() || value.IsNull() || !inherit.IsNull() {
			continue
		}
		planned := types.BoolValue(false)
		if value.IsUnknown() {
			// The value can still turn out null, and then the flag is not sent.
			planned = types.BoolUnknown()
		}
		diags.Append(plan.SetAttribute(ctx, path.Root("inherit_"+name), planned)...)
	}
	return diags
}

// sendInheritedSettings adds inherit_<setting> = false to fields, the body of a create or update, for every setting
// that config sets. Weblate clears the flag only when the stored value changes, so with a prior state that inherits,
// the setting is sent as well, even when it equals the stored value.
func sendInheritedSettings(ctx context.Context, config tfsdk.Config, plan tfsdk.Plan, prior *tfsdk.State, fields map[string]any) diag.Diagnostics {
	var diags diag.Diagnostics
	for name := range inheritedSettings {
		p := path.Root(name)
		var configured attr.Value
		if diags.Append(config.GetAttribute(ctx, p, &configured)...); diags.HasError() || configured.IsNull() {
			continue
		}
		if _, sent := fields[name]; !sent && prior != nil {
			var inherit types.Bool
			diags.Append(prior.GetAttribute(ctx, path.Root("inherit_"+name), &inherit)...)
			if inherit.Equal(types.BoolValue(false)) {
				continue
			}
			var planned attr.Value
			diags.Append(plan.GetAttribute(ctx, p, &planned)...)
			value, d := jsonValue(ctx, p, planned)
			diags.Append(d...)
			fields[name] = value
		}
		fields["inherit_"+name] = false
	}
	return diags
}
