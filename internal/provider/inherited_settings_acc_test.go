package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

// inheritedSettingsConfig returns a Project with license = "MIT", a Category and a Component at the Project root, each
// with the given extra attributes.
func inheritedSettingsConfig(project, projectExtra, categoryExtra, componentExtra string) string {
	return fmt.Sprintf(`
resource "weblate_project" "test" {
  name    = %[1]q
  slug    = %[1]q
  web     = "https://example.com"
  license = "MIT"
  %[2]s
}

resource "weblate_category" "test" {
  project = weblate_project.test.slug
  name    = "Category"
  slug    = "category"
  %[3]s
}

resource "weblate_component" "test" {
  project     = weblate_project.test.slug
  name        = "Component"
  slug        = "component"
  repo        = "http://git.test.example/demo.git"
  filemask    = "po/*.po"
  new_base    = "po/messages.pot"
  file_format = "po"
  %[4]s
}`, project, projectExtra, categoryExtra, componentExtra)
}

func expectAttribute(address, attribute string, value knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(address, tfjsonpath.New(attribute), value)
}

// checkEffectiveLicense checks the license that Weblate applies to the Component project/component, which the
// provider does not expose.
func checkEffectiveLicense(t *testing.T, project, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		comp, err := testAccClient(t).GetComponent(context.Background(), client.ComponentRef(project+"/component"))
		if err != nil {
			return err
		}
		if got := string(comp.Fields["effective_license"]); got != fmt.Sprintf("%q", want) {
			return fmt.Errorf("effective_license = %s, want %q", got, want)
		}
		return nil
	}
}

// TestAccInheritedSettings sets a value, switches to inheritance, switches back to the stored value and omits both,
// and the test framework checks after each step that the next plan is empty.
func TestAccInheritedSettings(t *testing.T) {
	project := uniqueSlug()
	emptyPlan := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project),
		Steps: []resource.TestStep{
			{
				Config: inheritedSettingsConfig(project, `enforced_checks = ["same"]`,
					`license = "MIT"
  enforced_checks = ["same"]`,
					`inherit_license = true
  commit_message  = "Translated using Weblate ({{ language_name }})"`),
				Check: checkEffectiveLicense(t, project, "MIT"),
				ConfigStateChecks: []statecheck.StateCheck{
					expectAttribute("weblate_project.test", "inherit_license", knownvalue.Bool(false)),
					expectAttribute("weblate_project.test", "enforced_checks",
						knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("same")})),
					expectAttribute("weblate_category.test", "inherit_license", knownvalue.Bool(false)),
					expectAttribute("weblate_category.test", "inherit_enforced_checks", knownvalue.Bool(false)),
					expectAttribute("weblate_component.test", "inherit_license", knownvalue.Bool(true)),
					expectAttribute("weblate_component.test", "inherit_commit_message", knownvalue.Bool(false)),
				},
			},
			{
				// new_lang = "add" equals the value that Weblate stored on create, while the Component inherits it.
				Config: inheritedSettingsConfig(project, "", `inherit_license = true`, `inherit_license = true
  inherit_commit_message = true
  new_lang               = "add"`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectAttribute("weblate_category.test", "inherit_license", knownvalue.Bool(true)),
					expectAttribute("weblate_category.test", "license", knownvalue.StringExact("MIT")),
					expectAttribute("weblate_component.test", "inherit_commit_message", knownvalue.Bool(true)),
					expectAttribute("weblate_component.test", "inherit_new_lang", knownvalue.Bool(false)),
				},
			},
			{
				Config: inheritedSettingsConfig(project, "", `license = "MIT"`, `license = "MIT"`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectAttribute("weblate_category.test", "inherit_license", knownvalue.Bool(false)),
					expectAttribute("weblate_component.test", "inherit_license", knownvalue.Bool(false)),
				},
			},
			{
				Config:           inheritedSettingsConfig(project, "", "", ""),
				ConfigPlanChecks: emptyPlan,
			},
			{
				Config:      inheritedSettingsConfig(project, "inherit_enforced_checks = true", "", ""),
				ExpectError: regexp.MustCompile(`Inheritance without a Workspace`),
			},
			{
				Config: inheritedSettingsConfig(project, `enforced_checks = ["same"]
  inherit_enforced_checks = true`, "", ""),
				ExpectError: regexp.MustCompile(`Inherited setting with its own value`),
			},
			{
				// The post-test destroy runs with the configuration of the last step, so it must be valid.
				Config: inheritedSettingsConfig(project, "", "", ""),
			},
		},
	})
}

// TestAccInheritedSettingsOfAProjectInAWorkspace needs the UUID of a Workspace of the test instance in
// WEBLATE_TEST_WORKSPACE, because a Project inherits only from its Workspace.
func TestAccInheritedSettingsOfAProjectInAWorkspace(t *testing.T) {
	workspace := os.Getenv("WEBLATE_TEST_WORKSPACE")
	if workspace == "" {
		t.Skip("WEBLATE_TEST_WORKSPACE is not set: the Weblate instance has no Workspace for this test")
	}
	slug := uniqueSlug()
	config := func(extra string) string {
		return projectConfig(slug, slug, fmt.Sprintf("workspace = %q\n  %s", workspace, extra))
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, slug),
		Steps: []resource.TestStep{
			{
				Config:            config(`license = "MIT"`),
				ConfigStateChecks: []statecheck.StateCheck{expectProject("inherit_license", knownvalue.Bool(false))},
			},
			{
				Config:            config("inherit_license = true"),
				ConfigStateChecks: []statecheck.StateCheck{expectProject("inherit_license", knownvalue.Bool(true))},
			},
			{
				Config:            config(`license = "MIT"`),
				ConfigStateChecks: []statecheck.StateCheck{expectProject("inherit_license", knownvalue.Bool(false))},
			},
			{
				Config:           config(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
		},
	})
}
