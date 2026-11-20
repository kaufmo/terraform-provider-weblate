package provider

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// readBackDiagnostics compares every top-level attribute that config sets with the value in stored, the state built
// from Weblate's response to a create or update. Weblate can store a value other than the one it received without
// an error, and without this check the user would get the framework's "Provider produced inconsistent result" or a
// diff on every plan instead of the attribute and both values. Attributes that the configuration omits are skipped,
// and string types with semantic equality, such as trimmedString, are compared by it.
func readBackDiagnostics(ctx context.Context, config tfsdk.Config, stored tfsdk.State) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, name := range slices.Sorted(maps.Keys(config.Schema.GetAttributes())) {
		p := path.Root(name)
		var sent, got attr.Value
		d := config.GetAttribute(ctx, p, &sent)
		diags.Append(d...)
		if d.HasError() || sent.IsNull() || sent.IsUnknown() {
			continue
		}
		d = stored.GetAttribute(ctx, p, &got)
		diags.Append(d...)
		if d.HasError() {
			continue
		}
		equal, d := semanticallyEqual(ctx, sent, got)
		diags.Append(d...)
		if !equal {
			diags.AddAttributeError(p, "Weblate stored a different value",
				fmt.Sprintf("The provider sent %s for %s, but Weblate stored %s. Weblate normalizes some values "+
					"and ignores others; set %s to the value that Weblate stores.", sent, name, got, name))
		}
	}
	return diags
}

func semanticallyEqual(ctx context.Context, sent, got attr.Value) (bool, diag.Diagnostics) {
	if s, ok := sent.(basetypes.StringValuableWithSemanticEquals); ok {
		if g, ok := got.(basetypes.StringValuable); ok {
			return s.StringSemanticEquals(ctx, g)
		}
	}
	return sent.Equal(got), nil
}
