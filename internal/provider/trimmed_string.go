package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var (
	_ basetypes.StringTypable                    = trimmedStringType{}
	_ basetypes.StringValuableWithSemanticEquals = trimmedString{}
)

// trimmedStringType is the type of every string attribute of weblate_project, weblate_category and weblate_component.
// Weblate strips leading and trailing whitespace from every string it receives, so a value that differs only in that
// whitespace, such as a heredoc ending in a newline, is the same value.
type trimmedStringType struct {
	basetypes.StringType
}

func (t trimmedStringType) Equal(o attr.Type) bool {
	other, ok := o.(trimmedStringType)
	return ok && t.StringType.Equal(other.StringType)
}

func (t trimmedStringType) String() string {
	return "trimmedStringType"
}

func (t trimmedStringType) ValueFromString(_ context.Context, in basetypes.StringValue) (basetypes.StringValuable, diag.Diagnostics) {
	return trimmedString{StringValue: in}, nil
}

func (t trimmedStringType) ValueFromTerraform(ctx context.Context, in tftypes.Value) (attr.Value, error) {
	v, err := t.StringType.ValueFromTerraform(ctx, in)
	if err != nil {
		return nil, err
	}
	s, ok := v.(basetypes.StringValue)
	if !ok {
		return nil, fmt.Errorf("unexpected value type %T", v)
	}
	return trimmedString{StringValue: s}, nil
}

func (t trimmedStringType) ValueType(context.Context) attr.Value {
	return trimmedString{}
}

type trimmedString struct {
	basetypes.StringValue
}

func trimmedStringValue(s string) trimmedString {
	return trimmedString{StringValue: basetypes.NewStringValue(s)}
}

func trimmedStringPointerValue(s *string) trimmedString {
	return trimmedString{StringValue: basetypes.NewStringPointerValue(s)}
}

func (v trimmedString) Equal(o attr.Value) bool {
	other, ok := o.(trimmedString)
	return ok && v.StringValue.Equal(other.StringValue)
}

func (v trimmedString) Type(context.Context) attr.Type {
	return trimmedStringType{}
}

func (v trimmedString) StringSemanticEquals(_ context.Context, o basetypes.StringValuable) (bool, diag.Diagnostics) {
	other, ok := o.(trimmedString)
	if !ok {
		return false, nil
	}
	return strings.TrimSpace(v.ValueString()) == strings.TrimSpace(other.ValueString()), nil
}
