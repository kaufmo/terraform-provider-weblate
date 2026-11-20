package provider

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

const groupAddress = "weblate_group.test"

func expectGroup(attribute string, value knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(groupAddress, tfjsonpath.New(attribute), value)
}

func TestAccGroupResource(t *testing.T) {
	name := uniqueSlug()
	renamed := name + "-renamed"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "weblate_group" "test" {
  name              = %q
  project_selection = "all_public"
}`, name),
				ConfigStateChecks: []statecheck.StateCheck{
					expectGroup("id", knownvalue.NotNull()),
					expectGroup("project", knownvalue.Null()),
					expectGroup("project_selection", knownvalue.StringExact("all_public")),
					expectGroup("language_selection", knownvalue.StringExact("manual")),
					expectGroup("enforced_2fa", knownvalue.Bool(false)),
				},
			},
			{
				Config: fmt.Sprintf(`
resource "weblate_group" "test" {
  name               = %q
  project_selection  = "all"
  language_selection = "all"
  enforced_2fa       = true
}`, renamed),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(groupAddress, plancheck.ResourceActionUpdate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					expectGroup("name", knownvalue.StringExact(renamed)),
					expectGroup("project_selection", knownvalue.StringExact("all")),
					expectGroup("language_selection", knownvalue.StringExact("all")),
					expectGroup("enforced_2fa", knownvalue.Bool(true)),
				},
			},
			{
				ResourceName:      groupAddress,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccGroupResourceProjectScoped(t *testing.T) {
	slug := uniqueSlug()
	renamed := slug + "-renamed"
	other := slug + "-other"
	config := func(projectSlug, groupProject string) string {
		return fmt.Sprintf(`
resource "weblate_project" "test" {
  name = %[1]q
  slug = %[2]q
  web  = "https://example.com"
}

resource "weblate_group" "test" {
  name    = %[1]q
  project = %[3]s
}`, slug, projectSlug, groupProject)
	}
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccClient(t)
			createTestProject(t, other)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, slug, renamed),
		Steps: []resource.TestStep{
			{
				Config: config(slug, "weblate_project.test.slug"),
				ConfigStateChecks: []statecheck.StateCheck{
					expectGroup("project", knownvalue.StringExact(slug)),
					expectGroup("project_selection", knownvalue.StringExact("manual")),
				},
			},
			{
				// A rename of the Project is no move: the Group follows it.
				Config: config(renamed, "weblate_project.test.slug"),
				ConfigStateChecks: []statecheck.StateCheck{
					expectGroup("project", knownvalue.StringExact(renamed)),
				},
			},
			{
				Config:      config(renamed, strconv.Quote(other)),
				ExpectError: regexp.MustCompile(`Cannot move the Group to another Project`),
			},
		},
	})
}

// TestAccGroupResourceImportBuiltIn imports the Administration Group that Weblate creates for every Project and
// manages its Roles.
func TestAccGroupResourceImportBuiltIn(t *testing.T) {
	slug := uniqueSlug()
	config := func(roles string) string {
		return fmt.Sprintf(`
resource "weblate_group" "builtin" {
  name    = "Administration"
  project = %q
  roles   = [%s]
}`, slug, roles)
	}
	expectRoles := func(roles ...string) statecheck.StateCheck {
		return statecheck.ExpectKnownValue("weblate_group.builtin", tfjsonpath.New("roles"), stringSetExact(roles...))
	}
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccClient(t)
			createTestProject(t, slug)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config(`"Administration"`),
				ResourceName:       "weblate_group.builtin",
				ImportState:        true,
				ImportStatePersist: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return builtInGroupID(t, slug, "Administration")
				},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 || states[0].Attributes["name"] != "Administration" ||
						states[0].Attributes["project"] != slug || states[0].Attributes["roles.#"] != "1" {
						return fmt.Errorf("imported %v, want the Administration Group of Project %s with one Role",
							states, slug)
					}
					return nil
				},
			},
			{
				Config:            config(`"Administration", "Review strings"`),
				ConfigStateChecks: []statecheck.StateCheck{expectRoles("Administration", "Review strings")},
			},
			{
				Config:            config(`"Administration"`),
				ConfigStateChecks: []statecheck.StateCheck{expectRoles("Administration")},
			},
		},
	})
}

func builtInGroupID(t *testing.T, slug, name string) (string, error) {
	groups, err := client.List[client.Group](context.Background(), testAccClient(t), "groups/")
	if err != nil {
		return "", err
	}
	for _, g := range groups {
		if g.Name == name && g.DefiningProject != nil && strings.HasSuffix(*g.DefiningProject, "/projects/"+slug+"/") {
			return strconv.FormatInt(g.ID, 10), nil
		}
	}
	return "", fmt.Errorf("project %s has no Group %s", slug, name)
}
