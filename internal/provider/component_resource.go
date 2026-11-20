package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

var (
	_ resource.ResourceWithConfigure      = &componentResource{}
	_ resource.ResourceWithImportState    = &componentResource{}
	_ resource.ResourceWithModifyPlan     = &componentResource{}
	_ resource.ResourceWithValidateConfig = &componentResource{}
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

var componentFields = withInheritedSettings(map[string]serializerField{
	"name": {kind: stringField, required: true, description: "Display name of the Component. Unique in the Project."},
	"slug": {kind: stringField, required: true, description: "Slug of the Component, as in its URL. Unique in the Project."},
	"repo": {kind: stringField, required: true, credentials: true, description: "URL of the repository that " +
		"Weblate clones, without credentials, or `weblate://` followed by the `id` of the Component to link to."},
	"filemask": {kind: stringField, required: true, path: true, description: "Path of the translation files relative to the repository root, with `*` instead of the language code, such as `po/*.po`."},
	"file_format": {kind: stringField, required: true, description: "File format of the translation files, such as " +
		"`po` or `json`."},
	"vcs": {kind: stringField, description: "Version control system, such as `git`. Defaults to `git`."},
	"vcs_params": {kind: jsonField, description: "Settings of the version control system as a JSON object, set " +
		"with `jsonencode`."},
	"branch": {kind: stringField, description: "Branch to translate. Defaults to the branch that `HEAD` of the " +
		"repository points to."},
	"push": {kind: stringField, credentials: true, description: "URL of the repository that Weblate pushes to, " +
		"without credentials. Weblate does not push when it is empty."},
	"push_branch": {kind: stringField, description: "Branch that Weblate pushes to. Empty means `branch`."},
	"git_export": {kind: stringField, computed: true, description: "URL from which users can fetch the changes " +
		"in Weblate's copy of the repository."},
	"repoweb": {kind: stringField, description: "Link to a repository browser for source files, with the " +
		"placeholders `{{branch}}`, `{{filename}}` and `{{line}}`."},
	"repoweb_translations": {kind: stringField, description: "Link to a repository browser for translation " +
		"files. Empty means `repoweb`."},
	"report_source_bugs": {kind: stringField, description: "E-mail address for reports on errors in source " +
		"strings."},
	"screenshot_filemask": {kind: stringField, path: true, description: "Path of screenshots relative to the " +
		"repository root, such as `docs/screenshots/*.png`."},
	"template": {kind: stringField, path: true, description: "Path of the base file of a monolingual format, which " +
		"holds the source strings."},
	"intermediate": {kind: stringField, path: true, description: "Path of the intermediate language file of a " +
		"monolingual format."},
	"new_base": {kind: stringField, path: true, description: "Path of the file from which Weblate creates new " +
		"translations, such as a `.pot` file."},
	"edit_template": {kind: boolField, description: "Whether users can edit the base file of a monolingual format. " +
		"Defaults to `true`. The converter formats `html`, `markdown`, `mdx`, `odf`, `idml`, `rc`, `txt`, " +
		"`dokuwiki`, `mediawiki`, `asciidoc` and `wxl` need `false`; the provider does not check this at plan."},
	"file_format_params": {kind: jsonField, description: "Settings of the file format as a JSON object, set with " +
		"`jsonencode`."},
	"check_flags": {kind: stringField, description: "Translation flags that customize the quality checks of the " +
		"Component."},
	"priority": {kind: int64Field, description: "Priority with which Weblate offers the Component to translators: " +
		"`60` very high, `80` high, `100` medium, `120` low, `140` very low. Defaults to `100`."},
	"restricted": {kind: boolField, description: "Whether only users with an explicit permission can access the " +
		"Component."},
	"merge_style": {kind: stringField, description: "How Weblate brings in upstream changes: `rebase`, `merge` or " +
		"`merge_noff`. Defaults to `rebase`."},
	"allow_translation_propagation": {kind: boolField, description: "Whether translations from other Components " +
		"of the Project with the same source string are used in this one. Defaults to `true`."},
	"hide_glossary_matches": {kind: boolField, description: "Whether the translation editor hides the glossary " +
		"panel."},
	"contribute_project_tm": {kind: boolField, description: "Whether the Component's translations go into the " +
		"translation memory of the Project. Defaults to `true`."},
	"manage_units": {kind: boolField, description: "Whether users can add, remove and edit source strings in " +
		"Weblate."},
	"enable_suggestions": {kind: boolField, description: "Whether users can make suggestions. Defaults to `true`."},
	"suggestion_voting":  {kind: boolField, description: "Whether users can vote on suggestions."},
	"suggestion_autoaccept": {kind: int64Field, description: "Number of votes after which Weblate accepts a " +
		"suggestion; `0` turns this off."},
	"push_on_commit": {kind: boolField, description: "Whether Weblate pushes after every commit. Defaults to " +
		"`true`."},
	"push_on_update": {kind: boolField, description: "Whether Weblate pushes after updating the repository, even " +
		"without new translations. Defaults to `true`."},
	"commit_pending_age": {kind: int64Field, description: "Hours after which Weblate commits pending changes. " +
		"Defaults to `24`."},
	"auto_lock_error": {kind: boolField, description: "Whether Weblate locks the Component on repository " +
		"errors. Defaults to `true`."},
	"language_regex": {kind: stringField, description: "Regular expression that selects the translation files " +
		"found by `filemask`. Defaults to `^[^.]+$`."},
	"key_filter": {kind: stringField, description: "Regular expression that selects the keys of a monolingual " +
		"format."},
	"variant_regex":  {kind: stringField, description: "Regular expression that finds the variants of a string."},
	"is_glossary":    {kind: boolField, description: "Whether the Component is a glossary."},
	"glossary_color": {kind: stringField, description: "Color of a glossary, such as `silver`, the default."},
})

func withInheritedSettings(fields map[string]serializerField) map[string]serializerField {
	maps.Copy(fields, inheritedSettingFields)
	return fields
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

// credentialAttributes returns the attributes that hold the credentials of the repository URL attribute name.
func credentialAttributes(name string) map[string]schema.Attribute {
	return map[string]schema.Attribute{
		name + "_username": schema.StringAttribute{
			MarkdownDescription: fmt.Sprintf("User name that the provider adds to `%[1]s`. A token login takes the "+
				"user name that the host expects, such as `x-access-token` for GitHub, `oauth2` for GitLab or "+
				"`x-token-auth` for Bitbucket. An SSH URL, such as `ssh://git@example.com/app.git`, needs `git` here "+
				"and no `%[1]s_password_wo`.", name),
			Optional: true,
		},
		name + "_password_wo": schema.StringAttribute{
			MarkdownDescription: fmt.Sprintf("Password or token that the provider adds to `%[1]s`. Needs "+
				"`%[1]s_username`. It is write-only: it never reaches plan or state, so the provider sends a new value "+
				"only when `%[1]s_password_wo_version` changes. Needs OpenTofu or Terraform 1.11 or later.", name),
			Optional:  true,
			WriteOnly: true,
		},
		name + "_password_wo_version": schema.Int64Attribute{
			MarkdownDescription: fmt.Sprintf("Version of `%[1]s_password_wo`. Change it, such as by adding 1, to send "+
				"`%[1]s` with the current `%[1]s_password_wo`.", name),
			Optional: true,
		},
	}
}

// withCredentials returns rawURL with the user name and password added, built with url.UserPassword so that special
// characters need no escaping in the configuration. Without a user name, it returns rawURL unchanged.
func withCredentials(rawURL string, username, password types.String) (string, error) {
	if username.IsNull() {
		return rawURL, nil
	}
	rawURL = strings.TrimSpace(rawURL)
	if u, err := url.Parse(rawURL); err != nil || u.Host == "" {
		return "", fmt.Errorf("the provider adds a user name only to a URL with a scheme and a host, such as "+
			"https://example.com/app.git or ssh://example.com/app.git, not to %q", rawURL)
	}
	user := url.User(username.ValueString())
	if !password.IsNull() {
		user = url.UserPassword(username.ValueString(), password.ValueString())
	}
	scheme, rest, _ := strings.Cut(rawURL, "//")
	return scheme + "//" + user.String() + "@" + rest, nil
}

func hasUserinfo(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && u.User != nil
}

// withoutUserinfo removes the user name and password from rawURL the way Weblate's cleanup_repo_url() does, for
// instances that set HIDE_REPO_CREDENTIALS = False and return the URL as stored.
func withoutUserinfo(rawURL string) string {
	if !hasUserinfo(rawURL) {
		return rawURL
	}
	_, rest, _ := strings.Cut(rawURL, "//")
	authority := rest[:strings.IndexAny(rest+"/", "/?#")]
	userinfo := authority[:strings.LastIndex(authority, "@")+1]
	return strings.Replace(rawURL, "//"+userinfo, "//", 1)
}

// sendCredentials replaces the repository URLs in fields, the body of a create or update, with the URLs that carry
// the credentials from config, and drops the credential attributes from it. With a prior state, it sends a URL only
// when the URL, its user name or its password version changed, because Weblate deletes the stored credentials when
// it receives the URL without them.
func sendCredentials(ctx context.Context, config tfsdk.Config, plan tfsdk.Plan, prior *tfsdk.State, fields map[string]any) diag.Diagnostics {
	var diags diag.Diagnostics
	for name, f := range componentFields {
		if !f.credentials {
			continue
		}
		for attribute := range credentialAttributes(name) {
			delete(fields, attribute)
		}
		var rawURL trimmedString
		var username, password types.String
		diags.Append(config.GetAttribute(ctx, path.Root(name), &rawURL)...)
		diags.Append(config.GetAttribute(ctx, path.Root(name+"_username"), &username)...)
		diags.Append(config.GetAttribute(ctx, path.Root(name+"_password_wo"), &password)...)
		if diags.HasError() || rawURL.IsNull() {
			continue
		}
		if _, urlChanged := fields[name]; prior != nil && !urlChanged {
			changed := false
			for _, attribute := range []string{name + "_username", name + "_password_wo_version"} {
				var planned, old attr.Value
				diags.Append(plan.GetAttribute(ctx, path.Root(attribute), &planned)...)
				diags.Append(prior.GetAttribute(ctx, path.Root(attribute), &old)...)
				changed = changed || !planned.Equal(old)
			}
			if !changed {
				continue
			}
		}
		withUser, err := withCredentials(rawURL.ValueString(), username, password)
		if err != nil {
			diags.AddAttributeError(path.Root(name+"_username"), "URL without host", err.Error())
			continue
		}
		fields[name] = withUser
	}
	return diags
}

// cleanPathValidator rejects a path that Weblate's cleanup_path() would change, because Weblate stores the cleaned
// path without an error and every plan would then show a diff.
type cleanPathValidator struct{}

func (cleanPathValidator) Description(context.Context) string {
	return "must not start with ./ or / and must not contain . or .. segments or //"
}

func (v cleanPathValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (cleanPathValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	p := strings.TrimSpace(req.ConfigValue.ValueString())
	if cleaned := cleanupPath(p); p != "" && cleaned != p {
		resp.Diagnostics.AddAttributeError(req.Path, "Path that Weblate changes",
			fmt.Sprintf("Weblate stores the path %q as %q. Set %s to %q.", p, cleaned, req.Path, cleaned))
	}
}

// cleanupPath does what Weblate's cleanup_path() does: it drops empty, "." and ".." segments instead of resolving
// them.
func cleanupPath(p string) string {
	var kept []string
	for _, segment := range strings.Split(p, "/") {
		if segment != "" && segment != "." && segment != ".." {
			kept = append(kept, segment)
		}
	}
	if len(kept) == 0 {
		return "."
	}
	return strings.Join(kept, "/")
}

type componentResource struct {
	client *client.Client
}

// NewComponentResource returns the weblate_component resource.
func NewComponentResource() resource.Resource {
	return &componentResource{}
}

func (r *componentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_component"
}

func (r *componentResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			MarkdownDescription: "The slug path of the Component: the Project slug, the slugs of its Categories and " +
				"its own slug, joined by `/`, such as `docs/guides/api`.",
			Computed: true,
		},
		"weblate_id": schema.Int64Attribute{
			MarkdownDescription: "Numeric ID that Weblate gave the Component. It stays the same when the " +
				"Component is renamed or moved, which lets the provider find a Component that was renamed outside " +
				"OpenTofu or whose Project was renamed outside OpenTofu.",
			Computed:      true,
			PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
		},
		"project": schema.StringAttribute{
			MarkdownDescription: "Slug of the Project that holds the Component.",
			Required:            true,
		},
		"category": schema.StringAttribute{
			MarkdownDescription: "`id` of the Category that holds the Component, which is its slug path and must " +
				"start with `project`. Without it, the Component is at the root of the Project.",
			Optional: true,
		},
		"source_language": schema.StringAttribute{
			MarkdownDescription: "Code of the language of the source strings, such as `en`, the default. Weblate " +
				"cannot change it after create, so a change fails at plan; apply with `-replace` to delete the " +
				"Component with its translations and create it again.",
			CustomType:    trimmedStringType{},
			Optional:      true,
			Computed:      true,
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"linked_component": schema.StringAttribute{
			MarkdownDescription: "`id` of the Component whose repository this Linked Component uses, or null. A " +
				"Component is linked only when `repo` is `weblate://` followed by that `id`.",
			Computed: true,
		},
	}
	for name, f := range componentFields {
		attributes[name] = f.attribute()
		if f.credentials {
			maps.Copy(attributes, credentialAttributes(name))
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Weblate Component, with its own repository or as a Linked Component.\n\n" +
			"Weblate strips leading and trailing whitespace from every string, so a value that differs from the " +
			"stored one only in that whitespace causes no change. An attribute that the configuration omits is " +
			"not managed: the provider reads it but never changes it. After create and update, the provider " +
			"compares every configured attribute with the value Weblate stored and fails the apply when they " +
			"differ; for `file_format_params` and `vcs_params` it names the keys that Weblate dropped or changed.\n\n" +
			"Changing `name` or `slug` renames the Component in place, and changing `category` moves it in place. " +
			"Weblate cannot move a Component to another Project, so changing `project` to another Project fails " +
			"on apply. When `project` changes because the Project was renamed, only the state changes.\n\n" +
			"Weblate processes a new or changed Component in a background task, such as cloning the repository " +
			"and parsing the files. The provider waits for the task. When the task fails, the Component stays in " +
			"Weblate and in state, the apply fails with the task's error and log, and after a create OpenTofu " +
			"replaces the Component on the next apply. Delete waits until Weblate has removed the Component.\n\n" +
			"A Component is a Linked Component, which uses the repository of another Component, only when `repo` " +
			"is `weblate://` followed by the `id` of that Component, such as `weblate://docs/guides/app`. Create " +
			"always sends `disable_autoshare`, so Weblate never links a Component because another Component has " +
			"the same repository. A Linked Component takes `push`, `branch`, `push_branch`, `vcs_params`, " +
			"`push_on_commit`, `push_on_update`, `commit_pending_age` and `auto_lock_error` from the Component " +
			"it links to, so setting one of them, or a `repo_*` or `push_*` credential attribute, fails at plan.\n\n" +
			"When the user of the API token lacks the permission `vcs.view` on the Component, such as with only " +
			"the Role Review strings or Translate, Weblate returns null for `vcs`, `vcs_params`, `repo`, `branch`, " +
			"`filemask`, `screenshot_filemask`, `push`, `git_export`, `push_branch`, `repoweb`, " +
			"`repoweb_translations` and `linked_component`. The provider then warns, keeps their values from " +
			"state, and cannot detect changes made to them outside OpenTofu. After an import with such a token " +
			"they stay null, so the next apply sends the configured values.\n\n" +
			"Weblate keeps repository credentials only inside the `repo` and `push` URLs and does not return " +
			"them. The provider adds `repo_username` and `repo_password_wo` to `repo`, and `push_username` and " +
			"`push_password_wo` to `push`; neither falls back to the other, and a URL with a user name or " +
			"password fails at plan. A changed password shows in the plan only when its `*_password_wo_version` " +
			"changes, and the provider cannot detect credentials that were changed outside OpenTofu. Weblate " +
			"deletes the stored credentials whenever it receives `repo` or `push` without them, so the provider " +
			"sends `repo` only when `repo`, `repo_username` or `repo_password_wo_version` changes, and then always " +
			"with the credentials from the configuration; the same applies to `push`. After an import, or when " +
			"the credentials were set in the Weblate UI, the first change of `repo` without `repo_password_wo` " +
			"deletes the stored credentials.\n\n" +
			inheritedSettingsDescription + "\n\n" +
			"Weblate adds objects of its own on create, which the provider ignores unless the configuration " +
			"declares them: it installs Add-ons that depend on the file format and the `DEFAULT_ADDONS` setting, " +
			"and for the first Component of a Project it creates a glossary Component with the slug `glossary`, " +
			"unless the Weblate setting `CREATE_GLOSSARIES` is off. To manage that glossary, import it, such as " +
			"`docs/glossary`.",
		Attributes: attributes,
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{
				Create: true,
				CreateDescription: "How long to wait for the background task of a create, such as `30m`. Defaults " +
					"to `20m`. When the time is up, the apply fails with the task's progress, the task goes on in " +
					"Weblate, and the Component is in state.",
				Update: true,
				UpdateDescription: "How long to wait for the background task of an update, such as `30m`. Defaults " +
					"to `20m`. When the time is up, the apply fails with the task's progress and the task goes on " +
					"in Weblate.",
				Delete: true,
				DeleteDescription: "How long to wait for Weblate to delete the Component in the background, such as " +
					"`30m`. Defaults to `20m`. When the time is up, the deletion goes on in Weblate.",
			}),
		},
	}
}

func (r *componentResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if c, ok := req.ProviderData.(*client.Client); ok {
		r.client = c
	}
}

// repoLinkPrefix starts the repo of a Linked Component, followed by the slug path of the Component it links to.
const repoLinkPrefix = "weblate://"

// linkedRepositorySettings are the attributes that a Linked Component takes from the Component it links to. Weblate
// returns that Component's values for them and drops or rejects values sent for the Linked Component.
var linkedRepositorySettings = []string{"push", "branch", "push_branch", "vcs_params", "push_on_commit",
	"push_on_update", "commit_pending_age", "auto_lock_error"}

// hiddenWithoutVCSView are the attributes that Weblate returns as null to a user without the permission vcs.view on
// the Component.
var hiddenWithoutVCSView = []string{"vcs", "vcs_params", "repo", "branch", "filemask", "screenshot_filemask", "push",
	"git_export", "push_branch", "repoweb", "repoweb_translations", "linked_component"}

// ValidateConfig rejects an Inherited setting with its own value, repository settings of a Linked Component and
// credentials that the provider cannot add to the repository URLs.
func (r *componentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	resp.Diagnostics.Append(validateInheritedSettings(ctx, req.Config)...)
	var repo trimmedString
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("repo"), &repo)...)
	if !repo.IsNull() && !repo.IsUnknown() && strings.HasPrefix(strings.TrimSpace(repo.ValueString()), repoLinkPrefix) {
		rejected := slices.Clone(linkedRepositorySettings)
		for name, f := range componentFields {
			if f.credentials {
				rejected = slices.AppendSeq(rejected, maps.Keys(credentialAttributes(name)))
			}
		}
		for _, name := range rejected {
			var v attr.Value
			resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(name), &v)...)
			if v != nil && !v.IsNull() {
				resp.Diagnostics.AddAttributeError(path.Root(name), "Repository setting of a Linked Component",
					fmt.Sprintf("With repo = %q, the Component uses the repository of the Component it links to and "+
						"takes %s from it. Remove %s, or set it on the linked Component.",
						strings.TrimSpace(repo.ValueString()), name, name))
			}
		}
		return
	}
	for name, f := range componentFields {
		if !f.credentials {
			continue
		}
		var rawURL trimmedString
		var username, password types.String
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(name), &rawURL)...)
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(name+"_username"), &username)...)
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(name+"_password_wo"), &password)...)
		if resp.Diagnostics.HasError() {
			return
		}
		urlKnown := !rawURL.IsNull() && !rawURL.IsUnknown()
		if urlKnown && hasUserinfo(rawURL.ValueString()) {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Credentials in the repository URL",
				fmt.Sprintf("Set the user name in %[1]s_username and the password or token in %[1]s_password_wo, "+
					"not in %[1]s. Weblate does not return the credentials in the URL, and %[1]s_password_wo keeps "+
					"them out of plan and state.", name))
			continue
		}
		if !password.IsNull() && username.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root(name+"_password_wo"), "Password without user name",
				fmt.Sprintf("%[1]s_password_wo needs %[1]s_username.", name))
		}
		if username.IsNull() || username.IsUnknown() {
			continue
		}
		if rawURL.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root(name+"_username"), "User name without URL",
				fmt.Sprintf("%[1]s_username needs %[1]s.", name))
		} else if _, err := withCredentials(rawURL.ValueString(), username, password); urlKnown && err != nil {
			resp.Diagnostics.AddAttributeError(path.Root(name+"_username"), "URL without host",
				fmt.Sprintf("%s_username needs %s with a host: %s.", name, name, err))
		}
	}
}

// ModifyPlan plans id as the new slug path and the inherit_<setting> flags, and rejects a change of
// source_language, which Weblate refuses.
func (r *componentResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	planSlugPath(ctx, resp)
	resp.Diagnostics.Append(planInheritedSettings(ctx, req.Config, &resp.Plan)...)
	if req.State.Raw.IsNull() {
		return
	}
	var planned, current trimmedString
	var id types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("source_language"), &planned)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("source_language"), &current)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	same, diags := planned.StringSemanticEquals(ctx, current)
	resp.Diagnostics.Append(diags...)
	if !planned.IsUnknown() && !current.IsNull() && !same {
		resp.Diagnostics.AddAttributeError(path.Root("source_language"), "Source language cannot change",
			fmt.Sprintf("Weblate cannot change the source language of the Component %s from %s to %s. To delete "+
				"the Component with its translations and create it again, apply with -replace for this resource.",
				id.ValueString(), current.ValueString(), planned.ValueString()))
	}
}

func (r *componentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	fields, diags := fieldsToWrite(ctx, req.Config, req.Plan, nil)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(sendCredentials(ctx, req.Config, req.Plan, nil, fields)...)
	resp.Diagnostics.Append(sendInheritedSettings(ctx, req.Config, req.Plan, nil, fields)...)
	var project, category types.String
	var t timeouts.Value
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("project"), &project)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("category"), &category)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("timeouts"), &t)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The Project is in the URL; Weblate's serializer has it read-only.
	delete(fields, "project")
	fields["disable_autoshare"] = true
	if !category.IsNull() {
		if fields["category"] = categoryURL(ctx, r.client, category, &resp.Diagnostics); resp.Diagnostics.HasError() {
			return
		}
	}
	comp, err := r.client.CreateComponent(ctx, project.ValueString(), fields)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Creating the Component failed", err))
		return
	}
	timeout, diags := t.Create(ctx, defaultTimeout)
	resp.Diagnostics.Append(diags...)
	r.finishWrite(ctx, req.Config, req.Plan, comp, t, timeout, &resp.State, &resp.Diagnostics)
}

// finishWrite waits for the background task of a create or update and puts the Component into state, also when the
// task failed or the wait timed out, because the write happened.
func (r *componentResource) finishWrite(ctx context.Context, config tfsdk.Config, plan tfsdk.Plan, comp *client.Component,
	t timeouts.Value, timeout time.Duration, state *tfsdk.State, diags *diag.Diagnostics) {
	var taskErr error
	if comp.TaskURL != "" {
		waitCtx, cancel := context.WithTimeout(ctx, timeout)
		taskErr = r.client.WaitForTask(waitCtx, comp.TaskURL)
		cancel()
		// Weblate built the response before the task ran.
		reread, err := r.client.GetComponent(ctx, comp.URL)
		if err != nil {
			diags.Append(apiErrorDiagnostic("Reading the Component failed", err))
		} else {
			comp = reread
		}
	}
	r.store(ctx, comp, t, plan, state, diags)
	// Weblate does not return the credentials, and a create starts from a null state.
	for name, f := range componentFields {
		if !f.credentials || state.Raw.IsNull() {
			continue
		}
		for attribute := range credentialAttributes(name) {
			var v attr.Value
			diags.Append(plan.GetAttribute(ctx, path.Root(attribute), &v)...)
			diags.Append(state.SetAttribute(ctx, path.Root(attribute), v)...)
		}
	}
	switch {
	case taskErr != nil:
		diags.Append(apiErrorDiagnostic("Processing the Component failed", taskErr))
	case !diags.HasError():
		diags.Append(readBackDiagnostics(ctx, config, *state)...)
	}
}

// attributeSource is the plan of a create or update, or the state of a read.
type attributeSource interface {
	GetAttribute(ctx context.Context, p path.Path, target any) diag.Diagnostics
}

// store puts the Component into state, with its slug path read from Weblate. prior holds the values that store keeps
// where Weblate does not return the Component's own: the repo of a Linked Component.
func (r *componentResource) store(ctx context.Context, comp *client.Component, t timeouts.Value, prior attributeSource,
	state *tfsdk.State, diags *diag.Diagnostics) {
	slugPath, err := r.client.ComponentSlugPath(ctx, comp)
	if err != nil {
		diags.Append(apiErrorDiagnostic("Reading the Categories of the Component failed", err))
		return
	}
	project, _, _ := strings.Cut(slugPath, "/")
	values := map[string]attr.Value{
		"id":              types.StringValue(slugPath),
		"weblate_id":      types.Int64Value(comp.ID),
		"project":         types.StringValue(project),
		"category":        parentFromSlugPath(slugPath, comp.Category != nil),
		"source_language": trimmedStringValue(comp.SourceLanguage.Code),
		"timeouts":        t,
	}
	for name, f := range componentFields {
		v, err := f.value(comp.Fields[name])
		if err != nil {
			diags.AddAttributeError(path.Root(name), "Unexpected value from Weblate",
				fmt.Sprintf("Weblate returned %s for %s: %s.", comp.Fields[name], name, err))
			return
		}
		values[name] = v
	}
	values["linked_component"] = types.StringNull()
	if comp.LinkedComponent != nil {
		linked, err := client.ComponentSlugPathFromURL(*comp.LinkedComponent)
		if err != nil {
			diags.AddAttributeError(path.Root("linked_component"), "Unexpected value from Weblate", err.Error())
			return
		}
		values["linked_component"] = types.StringValue(linked)
		// Weblate returns the repo of the linked Component.
		var repo trimmedString
		diags.Append(prior.GetAttribute(ctx, path.Root("repo"), &repo)...)
		link, isLink := strings.CutPrefix(strings.TrimSpace(repo.ValueString()), repoLinkPrefix)
		if !isLink || !strings.EqualFold(link, linked) {
			repo = trimmedStringValue(repoLinkPrefix + linked)
		}
		values["repo"] = repo
	}
	// vcs has a value in every Component, so null means that Weblate hid the repository settings.
	if string(comp.Fields["vcs"]) == "null" {
		for _, name := range hiddenWithoutVCSView {
			var v attr.Value
			diags.Append(prior.GetAttribute(ctx, path.Root(name), &v)...)
			// An unknown planned value stays null, the typed null from Weblate's response.
			if v != nil && !v.IsUnknown() {
				values[name] = v
			}
		}
		diags.AddWarning("Repository settings hidden by Weblate",
			fmt.Sprintf("Weblate returns null for %s of the Component %s, because the API token lacks the "+
				"permission vcs.view. The provider keeps their values from state, cannot detect changes made "+
				"outside OpenTofu, and skips them when it compares the configuration with what Weblate stored. "+
				"After an import, they stay null. Use a token whose user has a Role with vcs.view on the "+
				"Component, such as Power user or Administration.", strings.Join(hiddenWithoutVCSView, ", "), slugPath))
	}
	for name, v := range values {
		diags.Append(state.SetAttribute(ctx, path.Root(name), v)...)
	}
}

// locate reads the Component by its slug path and, when that returns 404, searches the Project for its numeric ID,
// so that a rename or move outside OpenTofu shows up as drift. It returns nil when the Component is gone.
func (r *componentResource) locate(ctx context.Context, slugPath, project types.String, weblateID types.Int64) (*client.Component, error) {
	comp, err := r.client.GetComponent(ctx, client.ComponentRef(slugPath.ValueString()))
	if !client.IsNotFound(err) {
		return comp, err
	}
	if weblateID.IsNull() {
		return nil, nil
	}
	return r.client.FindComponentByID(ctx, project.ValueString(), weblateID.ValueInt64())
}

func (r *componentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var id, project types.String
	var weblateID types.Int64
	var t timeouts.Value
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("project"), &project)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("weblate_id"), &weblateID)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("timeouts"), &t)...)
	if resp.Diagnostics.HasError() {
		return
	}
	comp, err := r.locate(ctx, id, project, weblateID)
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Reading the Component failed", err))
		return
	}
	if comp == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	r.store(ctx, comp, t, req.State, &resp.State, &resp.Diagnostics)
}

func (r *componentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// The framework stores the plan when Update fails. Until Weblate has the write, state keeps the prior values, so
	// that a bumped *_password_wo_version still differs on the retry and the URL with the credentials goes out again.
	resp.State.Raw = req.State.Raw
	fields, diags := fieldsToWrite(ctx, req.Config, req.Plan, &req.State)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(sendCredentials(ctx, req.Config, req.Plan, &req.State, fields)...)
	resp.Diagnostics.Append(sendInheritedSettings(ctx, req.Config, req.Plan, &req.State, fields)...)
	var id, project, planProject, category, planCategory types.String
	var weblateID types.Int64
	var t timeouts.Value
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("project"), &project)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("category"), &category)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("weblate_id"), &weblateID)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("project"), &planProject)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("category"), &planCategory)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("timeouts"), &t)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The Project cannot change; a Category is sent below as an API URL, and a move to the root as null.
	delete(fields, "project")
	delete(fields, "category")

	ref := client.ComponentRef(id.ValueString())
	var comp *client.Component
	var err error
	if !planProject.Equal(project) {
		// After a Project rename the old slug path returns 404, and the Component is in the renamed Project.
		if comp, err = r.locate(ctx, id, planProject, weblateID); err != nil {
			resp.Diagnostics.Append(apiErrorDiagnostic("Reading the Component failed", err))
			return
		}
		if comp == nil {
			resp.Diagnostics.AddError("Component not found",
				fmt.Sprintf("Weblate has no Component %s and no Component with the ID %d in the Project %s.",
					id.ValueString(), weblateID.ValueInt64(), planProject.ValueString()))
			return
		}
		// Slugs are unique, so the same slug means the same Project, renamed since the last refresh.
		if comp.Project.Slug != planProject.ValueString() {
			addProjectChangeError(&resp.Diagnostics, "Component", id.ValueString(), comp.Project.Slug,
				planProject.ValueString(), "its translations with their history")
			return
		}
		ref = comp.URL
	}
	// A Project rename changes the slug path of the Category but not the Category.
	sameCategory := planCategory.Equal(category) || (!planCategory.IsNull() && !category.IsNull() &&
		planCategory.ValueString() == planProject.ValueString()+
			strings.TrimPrefix(category.ValueString(), project.ValueString()))
	if !sameCategory {
		fields["category"] = nil
		if !planCategory.IsNull() {
			if fields["category"] = categoryURL(ctx, r.client, planCategory, &resp.Diagnostics); resp.Diagnostics.HasError() {
				return
			}
		}
	}
	switch {
	case len(fields) > 0:
		comp, err = r.client.UpdateComponent(ctx, ref, fields)
	case comp == nil:
		// Only the timeouts changed; a PATCH would still run Weblate's save logic.
		comp, err = r.client.GetComponent(ctx, ref)
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Updating the Component failed", err))
		return
	}
	timeout, diags := t.Update(ctx, defaultTimeout)
	resp.Diagnostics.Append(diags...)
	resp.State.Raw = req.Plan.Raw
	r.finishWrite(ctx, req.Config, req.Plan, comp, t, timeout, &resp.State, &resp.Diagnostics)
}

func (r *componentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var id types.String
	var t timeouts.Value
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("timeouts"), &t)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := t.Delete(ctx, defaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ref := client.ComponentRef(id.ValueString())
	err := r.client.DeleteComponent(ctx, ref)
	if client.IsNotFound(err) {
		return
	}
	if err == nil {
		err = r.client.WaitForDeletion(ctx, ref, "")
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic(fmt.Sprintf("Deleting the Component %s failed", id.ValueString()), err))
	}
}

// ImportState takes the slug path and finds the Component's numeric ID and Project, which Read needs.
func (r *componentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	comp, err := r.client.GetComponent(ctx, client.ComponentRef(req.ID))
	if client.IsNotFound(err) {
		resp.Diagnostics.AddError("Component not found", fmt.Sprintf("Weblate has no Component with the slug path "+
			"%s. The import ID is the slug path, such as docs/guides/api.", req.ID))
		return
	}
	if err != nil {
		resp.Diagnostics.Append(apiErrorDiagnostic("Finding the Component failed", err))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("weblate_id"), comp.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project"), comp.Project.Slug)...)
}
