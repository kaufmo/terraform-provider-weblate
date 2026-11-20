package provider

import (
	"context"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

// storedMo is the Add-on 4 on the Component docs/guides/app.
const (
	storedMo = `{"id": 4, "name": "weblate.gettext.mo", "project": null,
		"component": "http://weblate.example/api/components/docs/guides%252Fapp/",
		"configuration": {"fuzzy": true, "path": "{{ filename|stripext }}.mo"},
		"url": "http://weblate.example/api/addons/4/"}`
	moAlreadyInstalled = `{"type": "validation_error", "errors": [{"code": "invalid",
		"detail": "Add-on already installed: weblate.gettext.mo", "attr": "name"}]}`
	appAddonsPath = "/api/components/docs/guides%2Fapp/addons/"
)

var moConfig = map[string]any{"component": "docs/guides/app", "name": "weblate.gettext.mo",
	"configuration": `{"path":"{{ filename|stripext }}.mo","fuzzy":true}`}

func addonResourceWith(c *client.Client) (resource.Resource, resource.SchemaResponse) {
	ctx := context.Background()
	r := NewAddonResource()
	r.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: c}, &resource.ConfigureResponse{})
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	return r, schemaResp
}

func createAddon(t *testing.T, respond func(r request) (int, string), config map[string]any) (resource.CreateResponse, []request) {
	t.Helper()
	c, requests := fakeWeblate(t, respond)
	r, s := addonResourceWith(c)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s.Schema,
		Raw: tftypes.NewValue(s.Schema.Type().TerraformType(context.Background()), nil)}}
	newPrivate(&resp.Private)
	r.Create(context.Background(), resource.CreateRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:   tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, withoutWriteOnly(s, config))},
	}, &resp)
	return resp, *requests
}

// newPrivate initializes the private state of a response, as the framework does; its type is internal to the
// framework.
func newPrivate[T any](private **T) {
	*private = new(T)
}

func TestAddonCreateOnAComponent(t *testing.T) {
	resp, requests := createAddon(t, func(request) (int, string) { return http.StatusCreated, storedMo }, moConfig)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
	if len(requests) != 1 || requests[0].method != http.MethodPost || requests[0].path != appAddonsPath {
		t.Fatalf("requests = %v, want one POST %s", requests, appAddonsPath)
	}
	body := requests[0].body
	if conf, ok := body["configuration"].(map[string]any); len(body) != 2 || body["name"] != "weblate.gettext.mo" ||
		!ok || conf["fuzzy"] != true {
		t.Errorf("body = %v, want name and the configuration as a JSON object", body)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "4" {
		t.Errorf("id = %v, want 4", id)
	}
	if c := stateAttribute[types.String](t, resp.State, "component"); c.ValueString() != "docs/guides/app" {
		t.Errorf("component = %v, want docs/guides/app", c)
	}
	if p := stateAttribute[types.String](t, resp.State, "project"); !p.IsNull() {
		t.Errorf("project = %v, want null", p)
	}
}

func TestAddonCreateAdoptsAnInstalledAddon(t *testing.T) {
	addons := `{"next": null, "results": [` + strings.Replace(storedMo, `"fuzzy": true`, `"fuzzy": false`, 1) + `]}`
	for name, config := range map[string]map[string]any{
		"with configuration":    moConfig,
		"without configuration": {"component": "docs/guides/app", "name": "weblate.gettext.mo"},
	} {
		t.Run(name, func(t *testing.T) {
			resp, requests := createAddon(t, func(r request) (int, string) {
				switch {
				case r.method == http.MethodPost:
					return http.StatusBadRequest, moAlreadyInstalled
				case r.path == "/api/addons/":
					return http.StatusOK, addons
				}
				return http.StatusOK, storedMo
			}, config)
			if resp.Diagnostics.HasError() {
				t.Fatalf("Create: %v", resp.Diagnostics)
			}
			var patch *request
			for i, r := range requests {
				if r.method == http.MethodPatch {
					patch = &requests[i]
				}
			}
			if config["configuration"] == nil {
				if patch != nil {
					t.Errorf("requests = %v, want no PATCH without configuration", requests)
				}
				conf := stateAttribute[jsontypes.Normalized](t, resp.State, "configuration")
				if !strings.Contains(conf.ValueString(), `"fuzzy":false`) {
					t.Errorf("configuration = %v, want the installed one", conf)
				}
			} else if patch == nil || patch.path != "/api/addons/4/" || len(patch.body) != 1 || patch.body["configuration"] == nil {
				t.Errorf("requests = %v, want a PATCH /api/addons/4/ with only the configuration", requests)
			}
			if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "4" {
				t.Errorf("id = %v, want the adopted 4", id)
			}
		})
	}
}

func TestAddonCreateFailsWhenWeblateDropsAConfigurationKey(t *testing.T) {
	config := map[string]any{"project": "docs", "name": "weblate.gettext.mo",
		"configuration": `{"fuzzy":true,"fuzy":true}`}
	stored := strings.Replace(strings.Replace(storedMo, `"project": null`, `"project": "http://weblate.example/api/projects/docs/"`, 1),
		`"component": "http://weblate.example/api/components/docs/guides%252Fapp/"`, `"component": null`, 1)
	resp, requests := createAddon(t, func(request) (int, string) { return http.StatusCreated, stored }, config)
	if requests[0].path != "/api/projects/docs/addons/" {
		t.Errorf("requests = %v, want a POST /api/projects/docs/addons/", requests)
	}
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "keys fuzy") {
		t.Errorf("diagnostics = %v, want an error that names the key fuzy", resp.Diagnostics)
	}
	if id := stateAttribute[types.String](t, resp.State, "id"); id.ValueString() != "4" {
		t.Errorf("id = %v, want the created Add-on in state", id)
	}
}

func TestAddonConfigNeedsExactlyOneOfComponentAndProject(t *testing.T) {
	r, s := addonResourceWith(nil)
	for name, config := range map[string]map[string]any{
		"neither": {"name": "weblate.gettext.mo"},
		"both":    {"name": "weblate.gettext.mo", "component": "docs/app", "project": "docs"},
	} {
		var resp resource.ValidateConfigResponse
		r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
			Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)}}, &resp)
		if !resp.Diagnostics.HasError() {
			t.Errorf("%s: want an error", name)
		}
	}
}

func updateAddon(t *testing.T, respond func(r request) (int, string), prior, config map[string]any) (resource.UpdateResponse, []request) {
	t.Helper()
	c, requests := fakeWeblate(t, respond)
	r, s := addonResourceWith(c)
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)}}
	newPrivate(&resp.Private)
	r.Update(context.Background(), resource.UpdateRequest{
		Config:  tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)},
		Plan:    tfsdk.Plan{Schema: s.Schema, Raw: projectValue(s, withoutWriteOnly(s, config))},
		State:   tfsdk.State{Schema: s.Schema, Raw: projectValue(s, prior)},
		Private: resp.Private,
	}, &resp)
	return resp, *requests
}

func TestAddonUpdateOfTheComponent(t *testing.T) {
	prior := map[string]any{"id": "4", "component": "docs/app", "name": "weblate.gettext.mo",
		"configuration": `{"fuzzy":true,"path":"{{ filename|stripext }}.mo"}`}
	withComponent := func(component string) map[string]any {
		return map[string]any{"id": "4", "component": component, "name": "weblate.gettext.mo"}
	}

	// The Component docs/app was moved into the Category guides; the Add-on is the same.
	resp, requests := updateAddon(t, func(request) (int, string) { return http.StatusOK, storedMo }, prior,
		withComponent("docs/guides/app"))
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update after a move: %v", resp.Diagnostics)
	}
	for _, r := range requests {
		if r.method != http.MethodGet {
			t.Errorf("requests = %v, want only reads", requests)
		}
	}

	resp, _ = updateAddon(t, func(request) (int, string) { return http.StatusOK, storedMo }, prior,
		withComponent("docs/other"))
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "-replace") {
		t.Errorf("diagnostics = %v, want an error that points to -replace", resp.Diagnostics)
	}
}

func TestAddonUpdateRejectsANameChange(t *testing.T) {
	prior := map[string]any{"id": "4", "component": "docs/guides/app", "name": "weblate.gettext.mo"}
	config := map[string]any{"id": "4", "component": "docs/guides/app", "name": "weblate.gettext.linguas"}
	resp, requests := updateAddon(t, func(request) (int, string) { return http.StatusOK, storedMo }, prior, config)
	if !resp.Diagnostics.HasError() || len(requests) != 0 {
		t.Errorf("diagnostics = %v, requests = %v, want an error without requests", resp.Diagnostics, requests)
	}
}

// storedWebhook is the webhook Add-on 5 on the Project docs, with the secret in plain text as Weblate returns it.
const storedWebhook = `{"id": 5, "name": "weblate.webhook.webhook", "component": null,
	"project": "http://weblate.example/api/projects/docs/",
	"configuration": {"webhook_url": "https://hooks.example/weblate", "event_filter": "all",
		"secret": "whsec_c2VjcmV0LXNlY3JldC1zZWNyZXQtc2VjcmV0"},
	"url": "http://weblate.example/api/addons/5/"}`

var webhookConfig = map[string]any{"project": "docs", "name": "weblate.webhook.webhook",
	"configuration":                   `{"event_filter":"all","webhook_url":"https://hooks.example/weblate"}`,
	"secret_configuration_wo":         `{"secret":"whsec_c2VjcmV0LXNlY3JldC1zZWNyZXQtc2VjcmV0"}`,
	"secret_configuration_wo_version": 1}

func TestAddonConfigRejectsAKeyInConfigurationAndSecretConfiguration(t *testing.T) {
	r, s := addonResourceWith(nil)
	for name, config := range map[string]map[string]any{
		"same key": {"project": "docs", "name": "weblate.webhook.webhook",
			"configuration":           `{"secret":"a","webhook_url":"https://hooks.example/weblate"}`,
			"secret_configuration_wo": `{"secret":"b"}`},
		"no configuration": {"project": "docs", "name": "weblate.webhook.webhook",
			"secret_configuration_wo": `{"secret":"b"}`},
		"no JSON object": {"project": "docs", "name": "weblate.webhook.webhook", "configuration": `{}`,
			"secret_configuration_wo": `["secret"]`},
	} {
		var resp resource.ValidateConfigResponse
		r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
			Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, config)}}, &resp)
		if !resp.Diagnostics.HasError() {
			t.Errorf("%s: want an error", name)
		}
	}

	var resp resource.ValidateConfigResponse
	r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
		Config: tfsdk.Config{Schema: s.Schema, Raw: projectValue(s, webhookConfig)}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("separate keys: %v", resp.Diagnostics)
	}
}

func TestAddonCreateSendsTheSecretConfigurationAndKeepsItOutOfState(t *testing.T) {
	resp, requests := createAddon(t, func(request) (int, string) { return http.StatusCreated, storedWebhook }, webhookConfig)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
	body := requests[0].body
	conf, _ := body["configuration"].(map[string]any)
	if len(body) != 2 || conf["secret"] != "whsec_c2VjcmV0LXNlY3JldC1zZWNyZXQtc2VjcmV0" ||
		conf["webhook_url"] != "https://hooks.example/weblate" {
		t.Errorf("body = %v, want name and the configuration with the secret", body)
	}
	if c := stateAttribute[jsontypes.Normalized](t, resp.State, "configuration"); strings.Contains(c.ValueString(), "secret") {
		t.Errorf("configuration = %v, want it without the secret", c)
	}
	if v := stateAttribute[types.Int64](t, resp.State, "secret_configuration_wo_version"); v.ValueInt64() != 1 {
		t.Errorf("secret_configuration_wo_version = %v, want 1", v)
	}
}

func TestAddonCreateFailsWhenWeblateDropsASecretKey(t *testing.T) {
	config := maps.Clone(webhookConfig)
	config["secret_configuration_wo"] = `{"secrett":"whsec_c2VjcmV0LXNlY3JldC1zZWNyZXQtc2VjcmV0"}`
	resp, _ := createAddon(t, func(request) (int, string) { return http.StatusCreated, storedWebhook }, config)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "keys secrett") ||
		strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "whsec_") {
		t.Errorf("diagnostics = %v, want an error that names the key secrett without the secret", resp.Diagnostics)
	}
}

func TestAddonUpdateSendsTheSecretWithEveryConfiguration(t *testing.T) {
	_, s := addonResourceWith(nil)
	prior := withoutWriteOnly(s, webhookConfig)
	prior["id"] = "5"
	changedURL := maps.Clone(webhookConfig)
	changedURL["configuration"] = `{"event_filter":"all","webhook_url":"https://hooks.example/other"}`
	bumped := maps.Clone(webhookConfig)
	bumped["secret_configuration_wo_version"] = 2
	unchanged := maps.Clone(webhookConfig)
	unchanged["id"] = "5"

	for name, config := range map[string]map[string]any{"changed configuration": changedURL, "bumped version": bumped} {
		t.Run(name, func(t *testing.T) {
			config["id"] = "5"
			stored := strings.Replace(storedWebhook, "hooks.example/weblate", "hooks.example/"+
				map[string]string{"changed configuration": "other", "bumped version": "weblate"}[name], 1)
			resp, requests := updateAddon(t, func(request) (int, string) { return http.StatusOK, stored }, prior, config)
			if resp.Diagnostics.HasError() {
				t.Fatalf("Update: %v", resp.Diagnostics)
			}
			if len(requests) != 1 || requests[0].method != http.MethodPatch || len(requests[0].body) != 1 {
				t.Fatalf("requests = %v, want one PATCH with only the configuration", requests)
			}
			if conf, _ := requests[0].body["configuration"].(map[string]any); conf["secret"] == nil || conf["webhook_url"] == nil {
				t.Errorf("configuration = %v, want the configuration with the secret", conf)
			}
		})
	}

	unmanaged := map[string]any{"id": "5", "project": "docs", "name": "weblate.webhook.webhook",
		"secret_configuration_wo_version": 2}
	for name, config := range map[string]map[string]any{"no change": unchanged, "bumped version without configuration": unmanaged} {
		_, requests := updateAddon(t, func(request) (int, string) { return http.StatusOK, storedWebhook }, prior, config)
		for _, r := range requests {
			if r.method != http.MethodGet {
				t.Errorf("%s: requests = %v, want no write", name, requests)
			}
		}
	}
}

func TestAddonReadRemovesSecretKeysFromConfiguration(t *testing.T) {
	ctx := context.Background()
	stored := strings.Replace(storedWebhook, `"event_filter": "all"`, `"event_filter": "all", "token": "t0ken"`, 1)
	c, _ := fakeWeblate(t, func(request) (int, string) { return http.StatusOK, stored })
	r, s := addonResourceWith(c)

	read := func(private func(resp *resource.ReadResponse)) string {
		resp := resource.ReadResponse{State: tfsdk.State{Schema: s.Schema, Raw: projectValue(s, map[string]any{"id": "5"})}}
		newPrivate(&resp.Private)
		private(&resp)
		r.Read(ctx, resource.ReadRequest{State: resp.State, Private: resp.Private}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Read: %v", resp.Diagnostics)
		}
		return stateAttribute[jsontypes.Normalized](t, resp.State, "configuration").ValueString()
	}

	// After an import, only the built-in list of secret keys is known.
	if conf := read(func(*resource.ReadResponse) {}); strings.Contains(conf, "whsec_") || !strings.Contains(conf, "t0ken") {
		t.Errorf("configuration after an import = %s, want it without the webhook secret", conf)
	}
	// After a write, also the keys of secret_configuration_wo are removed.
	conf := read(func(resp *resource.ReadResponse) {
		resp.Diagnostics.Append(resp.Private.SetKey(ctx, "secret_configuration_keys", []byte(`["token"]`))...)
	})
	if strings.Contains(conf, "whsec_") || strings.Contains(conf, "t0ken") {
		t.Errorf("configuration after a write = %s, want it without secrets", conf)
	}
}
