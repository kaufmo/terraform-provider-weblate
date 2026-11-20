package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type fieldKind int

const (
	stringField fieldKind = iota
	boolField
	int64Field
	jsonField
	stringSetField
)

// serializerField is an attribute that maps one to one to the field of the same name in Weblate's serializer of
// the object, such as the Component serializer. Without required or computed, the attribute is Optional and
// Computed: Weblate has a default for it, and an omitted attribute is not managed.
type serializerField struct {
	kind        fieldKind
	description string
	required    bool
	computed    bool
	// path marks a file path that Weblate cleans with cleanup_path() before it stores it.
	path bool
	// credentials marks a repository URL that gets the attributes <name>_username, <name>_password_wo and
	// <name>_password_wo_version, because Weblate keeps the credentials only inside the URL.
	credentials bool
}

func (f serializerField) attribute() schema.Attribute {
	optional := !f.required && !f.computed
	computed := !f.required
	switch f.kind {
	case boolField:
		a := schema.BoolAttribute{MarkdownDescription: f.description, Required: f.required, Optional: optional,
			Computed: computed}
		if optional {
			a.PlanModifiers = []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}
		}
		return a
	case int64Field:
		a := schema.Int64Attribute{MarkdownDescription: f.description, Required: f.required, Optional: optional,
			Computed: computed}
		if optional {
			a.PlanModifiers = []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}
		}
		return a
	case stringSetField:
		a := schema.SetAttribute{MarkdownDescription: f.description, ElementType: types.StringType,
			Required: f.required, Optional: optional, Computed: computed}
		if optional {
			a.PlanModifiers = []planmodifier.Set{setplanmodifier.UseStateForUnknown()}
		}
		return a
	}
	a := schema.StringAttribute{MarkdownDescription: f.description, CustomType: trimmedStringType{},
		Required: f.required, Optional: optional, Computed: computed}
	if f.kind == jsonField {
		a.CustomType = jsontypes.NormalizedType{}
	}
	if optional {
		a.PlanModifiers = []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	}
	if f.path {
		a.Validators = []validator.String{cleanPathValidator{}}
	}
	return a
}

// value converts the field from a Weblate response into the attribute value. A missing field is null.
func (f serializerField) value(raw json.RawMessage) (attr.Value, error) {
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	switch f.kind {
	case boolField:
		var b *bool
		err := json.Unmarshal(raw, &b)
		return types.BoolPointerValue(b), err
	case int64Field:
		var i *int64
		err := json.Unmarshal(raw, &i)
		return types.Int64PointerValue(i), err
	case stringSetField:
		var elements []string
		if err := json.Unmarshal(raw, &elements); err != nil || elements == nil {
			return types.SetNull(types.StringType), err
		}
		set, diags := types.SetValueFrom(context.Background(), types.StringType, elements)
		if diags.HasError() {
			return set, fmt.Errorf("%v", diags)
		}
		return set, nil
	case jsonField:
		var v any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if err := d.Decode(&v); err != nil || v == nil {
			return jsontypes.NewNormalizedNull(), err
		}
		// Sorted keys, as jsonencode writes them, so that an imported value equals the configured one.
		sorted, err := json.Marshal(v)
		return jsontypes.NewNormalizedValue(string(sorted)), err
	}
	var s *string
	err := json.Unmarshal(raw, &s)
	if s != nil && f.credentials {
		*s = withoutUserinfo(*s)
	}
	return trimmedStringPointerValue(s), err
}

func (f serializerField) dataSourceAttribute() dsschema.Attribute {
	switch f.kind {
	case boolField:
		return dsschema.BoolAttribute{MarkdownDescription: f.description, Computed: true}
	case int64Field:
		return dsschema.Int64Attribute{MarkdownDescription: f.description, Computed: true}
	case stringSetField:
		return dsschema.SetAttribute{MarkdownDescription: f.description, Computed: true, ElementType: types.StringType}
	case jsonField:
		return dsschema.StringAttribute{MarkdownDescription: f.description, Computed: true,
			CustomType: jsontypes.NormalizedType{}}
	}
	return dsschema.StringAttribute{MarkdownDescription: f.description, Computed: true, CustomType: trimmedStringType{}}
}
