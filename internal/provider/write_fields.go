package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// fieldsToWrite returns the request body of a create or update: the planned value of every top-level attribute that
// config sets, keyed by attribute name, which is the field name in Weblate's serializer. With a prior state, only
// attributes whose planned value differs from it are included, so an update leaves every other field alone. An
// attribute that config omits is not managed and never sent.
func fieldsToWrite(ctx context.Context, config tfsdk.Config, plan tfsdk.Plan, prior *tfsdk.State) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	fields := map[string]any{}
	for name := range config.Schema.GetAttributes() {
		p := path.Root(name)
		var configured, planned attr.Value
		diags.Append(config.GetAttribute(ctx, p, &configured)...)
		diags.Append(plan.GetAttribute(ctx, p, &planned)...)
		if diags.HasError() {
			return nil, diags
		}
		if configured.IsNull() {
			continue
		}
		if prior != nil {
			var old attr.Value
			if diags.Append(prior.GetAttribute(ctx, p, &old)...); diags.HasError() {
				return nil, diags
			}
			if planned.Equal(old) {
				continue
			}
		}
		value, d := jsonValue(ctx, p, planned)
		diags.Append(d...)
		fields[name] = value
	}
	return fields, diags
}

func jsonValue(ctx context.Context, p path.Path, v attr.Value) (any, diag.Diagnostics) {
	switch v := v.(type) {
	case jsontypes.Normalized:
		// Weblate's JSON fields store a string as a string, so the object has to be sent decoded.
		var decoded any
		diags := v.Unmarshal(&decoded)
		return decoded, diags
	case basetypes.StringValuable:
		s, diags := v.ToStringValue(ctx)
		return s.ValueString(), diags
	case basetypes.BoolValuable:
		b, diags := v.ToBoolValue(ctx)
		return b.ValueBool(), diags
	case basetypes.Int64Valuable:
		i, diags := v.ToInt64Value(ctx)
		return i.ValueInt64(), diags
	}
	var diags diag.Diagnostics
	diags.AddAttributeError(p, "Unsupported attribute type",
		fmt.Sprintf("The provider cannot send a value of type %T to Weblate. This is a bug in the provider.", v))
	return nil, diags
}
