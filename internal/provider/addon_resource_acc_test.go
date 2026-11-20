package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// addonConfig returns componentConfig with the Component in the Category a and the given weblate_addon resources.
// The test git server is read-only, so the Component does not push the commits that an Add-on can cause.
func addonConfig(project, addons string) string {
	return componentConfig(project, "a", "test", "push_on_commit = false") + addons
}

func moAddon(owner, configuration string) string {
	return fmt.Sprintf(`
resource "weblate_addon" "test" {
  %s
  name = "weblate.gettext.mo"
  %s
}`, owner, configuration)
}

const onComponent = "component = weblate_component.test.id"

func expectAddon(attribute string, value knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue("weblate_addon.test", tfjsonpath.New(attribute), value)
}

func TestAccAddonResourceOnAComponent(t *testing.T) {
	project := uniqueSlug()
	sameID := statecheck.CompareValue(compare.ValuesSame())

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project),
		Steps: []resource.TestStep{
			{
				Config: addonConfig(project, moAddon(onComponent, "")),
				ConfigStateChecks: []statecheck.StateCheck{
					sameID.AddStateValue("weblate_addon.test", tfjsonpath.New("id")),
					expectAddon("component", knownvalue.StringExact(project+"/a/test")),
					expectAddon("project", knownvalue.Null()),
					expectAddon("configuration", knownvalue.NotNull()),
				},
			},
			{
				Config: addonConfig(project, moAddon(onComponent,
					`configuration = jsonencode({ fuzzy = true, path = "{{ filename|stripext }}.mo" })`)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("weblate_addon.test", plancheck.ResourceActionUpdate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					sameID.AddStateValue("weblate_addon.test", tfjsonpath.New("id")),
				},
			},
			{
				ResourceName:      "weblate_addon.test",
				ImportState:       true,
				ImportStateVerify: true,
				// configuration keeps the configured spelling in state; the import reads Weblate's.
				ImportStateVerifyIgnore: []string{"configuration"},
			},
			{
				// The Component moves to the Category b; the Add-on goes with it.
				Config: componentConfig(project, "b", "test", "push_on_commit = false") + moAddon(onComponent, ""),
				ConfigStateChecks: []statecheck.StateCheck{
					sameID.AddStateValue("weblate_addon.test", tfjsonpath.New("id")),
					expectAddon("component", knownvalue.StringExact(project+"/b/test")),
				},
			},
			{
				// Weblate drops a key that the Add-on does not know.
				Config: componentConfig(project, "b", "test", "push_on_commit = false") + moAddon(onComponent,
					`configuration = jsonencode({ fuzy = true })`),
				ExpectError: regexp.MustCompile(`keys fuzy`),
			},
		},
	})
}

func TestAccAddonResourceOnAProject(t *testing.T) {
	project := uniqueSlug()
	onProject := "project = weblate_project.test.slug"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project),
		Steps: []resource.TestStep{
			{
				Config: addonConfig(project, moAddon(onProject, `configuration = jsonencode({ fuzzy = false })`)),
				ConfigStateChecks: []statecheck.StateCheck{
					expectAddon("project", knownvalue.StringExact(project)),
					expectAddon("component", knownvalue.Null()),
				},
			},
			{
				Config: addonConfig(project, moAddon(onProject, `configuration = jsonencode({ fuzzy = true })`)),
				ConfigStateChecks: []statecheck.StateCheck{
					expectAddon("configuration", knownvalue.StringExact(`{"fuzzy":true}`)),
				},
			},
			{
				ResourceName:            "weblate_addon.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"configuration"},
			},
		},
	})
}

func TestAccAddonResourceAdoptsAnInstalledAddon(t *testing.T) {
	project := uniqueSlug()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project),
		Steps: []resource.TestStep{
			{
				// Weblate installs weblate.flags.same_edit on its own when it creates a Component in the srt format.
				Config: fmt.Sprintf(`
resource "weblate_project" "test" {
  name = %[1]q
  slug = %[1]q
  web  = "https://example.com"
}

resource "weblate_component" "test" {
  project        = weblate_project.test.slug
  name           = "Subtitles"
  slug           = "subtitles"
  repo           = "http://git.test.example/demo.git"
  filemask       = "srt/*.srt"
  template       = "srt/en.srt"
  file_format    = "srt"
  push_on_commit = false
  # Without a new_base file, Weblate rejects the default new_lang = "add".
  new_lang       = "none"
}

resource "weblate_addon" "test" {
  component = weblate_component.test.id
  name      = "weblate.flags.same_edit"
}`, project),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					expectAddon("component", knownvalue.StringExact(project+"/subtitles")),
				},
			},
		},
	})
}

func TestAccAddonResourceInstalledTwice(t *testing.T) {
	project := uniqueSlug()
	generate := func(name, filename string) string {
		return fmt.Sprintf(`
resource "weblate_addon" %q {
  component     = weblate_component.test.id
  name          = "weblate.generate.generate"
  configuration = jsonencode({ filename = %q, template = "{{ stats.translated }}" })
}`, name, filename)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project),
		Steps: []resource.TestStep{
			{
				Config: addonConfig(project, generate("one", "one/{{ language_code }}.txt")+
					generate("two", "two/{{ language_code }}.txt")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.CompareValuePairs("weblate_addon.one", tfjsonpath.New("id"),
						"weblate_addon.two", tfjsonpath.New("id"), compare.ValuesDiffer()),
				},
			},
		},
	})
}

// Webhook secrets are base64 strings that decode to 24 to 64 bytes.
const (
	webhookSecret      = "whsec_MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	otherWebhookSecret = "whsec_ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA="
)

// webhookAddon points the webhook to the test git server, which the settings override allows as a private target.
func webhookAddon(path, secret string, version int) string {
	return fmt.Sprintf(`
resource "weblate_addon" "test" {
  project       = weblate_project.test.slug
  name          = "weblate.webhook.webhook"
  configuration = jsonencode({ webhook_url = "http://git.test.example/%s", event_filter = "all", events = [] })

  secret_configuration_wo         = jsonencode({ secret = %q })
  secret_configuration_wo_version = %d
}`, path, secret, version)
}

// checkStoredWebhookSecret checks the secret in the configuration that Weblate stores, which state never has.
func checkStoredWebhookSecret(t *testing.T, want string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		id, err := strconv.ParseInt(s.RootModule().Resources["weblate_addon.test"].Primary.ID, 10, 64)
		if err != nil {
			return err
		}
		a, err := testAccClient(t).GetAddon(context.Background(), id)
		if err != nil {
			return err
		}
		var configuration struct{ Secret string }
		if err := json.Unmarshal(a.Configuration, &configuration); err != nil {
			return err
		}
		if configuration.Secret != want {
			return fmt.Errorf("weblate stored the secret %q, want %q", configuration.Secret, want)
		}
		return nil
	}
}

func TestAccAddonResourceWebhookSecret(t *testing.T) {
	project := uniqueSlug()
	withoutSecret := func(path string) knownvalue.Check {
		return knownvalue.StringExact(`{"event_filter":"all","events":[],"webhook_url":"http://git.test.example/` + path + `"}`)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project),
		Steps: []resource.TestStep{
			{
				Config: addonConfig(project, webhookAddon("hook", webhookSecret, 1)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectKnownValue("weblate_addon.test", tfjsonpath.New("secret_configuration_wo"),
						knownvalue.Null()),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					expectAddon("configuration", withoutSecret("hook")),
					expectAddon("secret_configuration_wo", knownvalue.Null()),
				},
				Check: checkStoredWebhookSecret(t, webhookSecret),
			},
			{
				// A changed secret is sent only with a new version, but every configuration change sends the
				// configured one.
				Config:            addonConfig(project, webhookAddon("other", otherWebhookSecret, 1)),
				ConfigStateChecks: []statecheck.StateCheck{expectAddon("configuration", withoutSecret("other"))},
				Check:             checkStoredWebhookSecret(t, otherWebhookSecret),
			},
			{
				Config: addonConfig(project, webhookAddon("other", webhookSecret, 2)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("weblate_addon.test", plancheck.ResourceActionUpdate),
				}},
				Check: checkStoredWebhookSecret(t, webhookSecret),
			},
			{
				ResourceName:            "weblate_addon.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secret_configuration_wo_version"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 || strings.Contains(states[0].Attributes["configuration"], "secret") {
						return fmt.Errorf("imported %v, want a configuration without the secret", states)
					}
					return nil
				},
			},
		},
	})
}
