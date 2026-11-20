package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
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
				Config: addonConfig(project, ""),
			},
			{
				// Stands in for an Add-on that Weblate installs on its own: the po format of the test repository
				// gets none.
				PreConfig: func() {
					_, err := testAccClient(t).CreateAddon(context.Background(),
						client.ComponentRef(project+"/a/test"), map[string]any{"name": "weblate.gettext.mo"})
					if err != nil {
						t.Fatalf("installing the Add-on: %v", err)
					}
				},
				Config: addonConfig(project, moAddon(onComponent, `configuration = jsonencode({ fuzzy = true })`)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					expectAddon("configuration", knownvalue.StringExact(`{"fuzzy":true}`)),
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
