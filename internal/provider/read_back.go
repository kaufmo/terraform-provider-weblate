package provider

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
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
// and string types with semantic equality, such as trimmedString, are compared by it. Write-only attributes never
// reach state and are skipped.
func readBackDiagnostics(ctx context.Context, config tfsdk.Config, stored tfsdk.State) diag.Diagnostics {
	var diags diag.Diagnostics
	attributes := config.Schema.GetAttributes()
	for _, name := range slices.Sorted(maps.Keys(attributes)) {
		if attributes[name].IsWriteOnly() {
			continue
		}
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
					"and ignores others; set %s to the value that Weblate stores.%s", sent, name, got, name,
					changedKeysSentence(sent, got)))
		}
	}
	return diags
}

// changedKeysSentence names the top-level keys of a JSON object attribute that Weblate dropped or changed, because
// Weblate drops unknown keys without an error and a misspelled key is hard to spot in two long JSON strings.
func changedKeysSentence(sent, got attr.Value) string {
	s, ok1 := sent.(jsontypes.Normalized)
	g, ok2 := got.(jsontypes.Normalized)
	var sentKeys, gotKeys map[string]any
	if !ok1 || !ok2 || s.Unmarshal(&sentKeys) != nil || g.Unmarshal(&gotKeys) != nil {
		return ""
	}
	var changed []string
	for _, key := range slices.Sorted(maps.Keys(sentKeys)) {
		if stored, ok := gotKeys[key]; !ok || !reflect.DeepEqual(stored, sentKeys[key]) {
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		return ""
	}
	return " Weblate dropped or changed the keys " + strings.Join(changed, ", ") + "."
}

func semanticallyEqual(ctx context.Context, sent, got attr.Value) (bool, diag.Diagnostics) {
	if s, ok := sent.(basetypes.StringValuableWithSemanticEquals); ok {
		if g, ok := got.(basetypes.StringValuable); ok {
			return s.StringSemanticEquals(ctx, g)
		}
	}
	return sent.Equal(got), nil
}
