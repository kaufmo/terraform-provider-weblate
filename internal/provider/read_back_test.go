package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var readBackSchema = schema.Schema{Attributes: map[string]schema.Attribute{
	"id":             schema.StringAttribute{Computed: true},
	"name":           schema.StringAttribute{CustomType: trimmedStringType{}, Required: true},
	"instructions":   schema.StringAttribute{CustomType: trimmedStringType{}, Optional: true, Computed: true},
	"access_control": schema.Int64Attribute{Optional: true, Computed: true},
	"enable_hooks":   schema.BoolAttribute{Optional: true, Computed: true},
	"file_format_params": schema.StringAttribute{CustomType: jsontypes.NormalizedType{}, Optional: true,
		Computed: true},
}}

// readBackObject returns a value of readBackSchema with the given attributes and null for the others.
func readBackObject(attributes map[string]any) tftypes.Value {
	objectType := readBackSchema.Type().TerraformType(context.Background()).(tftypes.Object)
	values := map[string]tftypes.Value{}
	for name, typ := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(typ, attributes[name])
	}
	return tftypes.NewValue(objectType, values)
}

func readBack(config, stored map[string]any) (errs []string, paths []path.Path) {
	diags := readBackDiagnostics(context.Background(),
		tfsdk.Config{Schema: readBackSchema, Raw: readBackObject(config)},
		tfsdk.State{Schema: readBackSchema, Raw: readBackObject(stored)})
	for _, d := range diags.Errors() {
		errs = append(errs, d.Summary()+": "+d.Detail())
		if withPath, ok := d.(interface{ Path() path.Path }); ok {
			paths = append(paths, withPath.Path())
		}
	}
	return errs, paths
}

func TestReadBackAcceptsTheStoredValues(t *testing.T) {
	errs, _ := readBack(
		map[string]any{"name": "Docs", "instructions": "Read the style guide.\n", "enable_hooks": false},
		map[string]any{"id": "docs", "name": "Docs", "instructions": "Read the style guide.", "enable_hooks": false,
			"access_control": int64(100)},
	)
	if errs != nil {
		t.Errorf("diagnostics = %v, want none", errs)
	}
}

func TestReadBackNamesTheAttributeAndBothValues(t *testing.T) {
	errs, paths := readBack(
		map[string]any{"name": "Docs", "access_control": int64(100)},
		map[string]any{"name": "Docs", "access_control": int64(0)},
	)
	if len(errs) != 1 {
		t.Fatalf("diagnostics = %v, want one error", errs)
	}
	if len(paths) != 1 || !paths[0].Equal(path.Root("access_control")) {
		t.Errorf("paths = %v, want access_control", paths)
	}
	for _, want := range []string{"100", "0"} {
		if !strings.Contains(errs[0], want) {
			t.Errorf("error %q does not contain %q", errs[0], want)
		}
	}
}

func TestReadBackComparesStringsWithoutTrimmedWhitespace(t *testing.T) {
	errs, paths := readBack(
		map[string]any{"name": "Docs\n"},
		map[string]any{"name": "Documentation"},
	)
	if len(errs) != 1 || !paths[0].Equal(path.Root("name")) {
		t.Fatalf("diagnostics = %v at %v, want one error on name", errs, paths)
	}
	if !strings.Contains(errs[0], `"Docs\n"`) || !strings.Contains(errs[0], `"Documentation"`) {
		t.Errorf("error %q does not quote both values", errs[0])
	}
}

func TestReadBackReportsEveryDifferentAttribute(t *testing.T) {
	errs, _ := readBack(
		map[string]any{"name": "Docs", "enable_hooks": true, "access_control": int64(1)},
		map[string]any{"name": "Docs", "enable_hooks": false, "access_control": int64(0)},
	)
	if len(errs) != 2 {
		t.Errorf("diagnostics = %v, want two errors", errs)
	}
}

func TestReadBackIgnoresJSONKeyOrderAndNamesChangedKeys(t *testing.T) {
	if errs, _ := readBack(
		map[string]any{"name": "Docs", "file_format_params": `{"b": 2, "a": 1}`},
		map[string]any{"name": "Docs", "file_format_params": `{"a":1,"b":2}`},
	); errs != nil {
		t.Errorf("diagnostics = %v, want none for another key order", errs)
	}

	errs, paths := readBack(
		map[string]any{"name": "Docs", "file_format_params": `{"po_line_wrap": 77, "po_typo": true, "keep": "x"}`},
		map[string]any{"name": "Docs", "file_format_params": `{"po_line_wrap": "77", "keep": "x"}`},
	)
	if len(errs) != 1 || !paths[0].Equal(path.Root("file_format_params")) {
		t.Fatalf("diagnostics = %v at %v, want one error on file_format_params", errs, paths)
	}
	if !strings.Contains(errs[0], "po_line_wrap, po_typo.") {
		t.Errorf("error %q does not name exactly the keys po_line_wrap and po_typo", errs[0])
	}
}
