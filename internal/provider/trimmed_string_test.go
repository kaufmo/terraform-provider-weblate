package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestTrimmedStringEqualsTheTrimmedValue(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		configured, stored string
		want               bool
	}{
		{"Read the style guide.\n", "Read the style guide.", true},
		{"  docs ", "docs", true},
		{"docs", "docs", true},
		{"docs", "Docs", false},
		{"a\n\nb", "a\nb", false},
	} {
		equal, diags := trimmedStringValue(tc.configured).StringSemanticEquals(ctx, trimmedStringValue(tc.stored))
		if diags.HasError() {
			t.Fatal(diags)
		}
		if equal != tc.want {
			t.Errorf("%q equals %q = %v, want %v", tc.configured, tc.stored, equal, tc.want)
		}
	}
}

func TestTrimmedStringTypeReadsTerraformValues(t *testing.T) {
	ctx := context.Background()
	v, err := trimmedStringType{}.ValueFromTerraform(ctx, tftypes.NewValue(tftypes.String, "docs\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := v.(trimmedString); !ok || got.ValueString() != "docs\n" {
		t.Errorf("ValueFromTerraform = %#v, want the trimmedString docs\\n", v)
	}
	null, err := trimmedStringType{}.ValueFromTerraform(ctx, tftypes.NewValue(tftypes.String, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !null.IsNull() {
		t.Errorf("ValueFromTerraform(nil) = %v, want null", null)
	}
}
