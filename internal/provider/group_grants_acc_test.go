package provider

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

// createTestComponentList creates an empty Component list through the client and deletes it when the test ends.
func createTestComponentList(t *testing.T, slug string) {
	t.Helper()
	c := testAccClient(t)
	ctx := context.Background()
	if err := c.Do(ctx, http.MethodPost, "component-lists/", map[string]string{"name": slug, "slug": slug}, nil); err != nil {
		t.Fatalf("creating Component list %s: %v", slug, err)
	}
	t.Cleanup(func() {
		if err := c.Do(ctx, http.MethodDelete, "component-lists/"+slug+"/", nil, nil); err != nil && !client.IsNotFound(err) {
			t.Errorf("deleting Component list %s: %v", slug, err)
		}
	})
}

// addGrantOutside grants key to the test Group through the client, as a change in the Weblate UI would.
func addGrantOutside(t *testing.T, grant client.GrantType, key any) func(*terraform.State) error {
	return func(s *terraform.State) error {
		id, err := groupIDFromState(s)
		if err != nil {
			return err
		}
		return testAccClient(t).AddGrant(context.Background(), id, grant, key)
	}
}

func groupIDFromState(s *terraform.State) (int64, error) {
	r, ok := s.RootModule().Resources[groupAddress]
	if !ok {
		return 0, fmt.Errorf("no %s in state", groupAddress)
	}
	var id int64
	_, err := fmt.Sscan(r.Primary.ID, &id)
	return id, err
}

func stringSetExact(values ...string) knownvalue.Check {
	checks := []knownvalue.Check{}
	for _, v := range values {
		checks = append(checks, knownvalue.StringExact(v))
	}
	return knownvalue.SetExact(checks)
}

func TestAccGroupResourceGrants(t *testing.T) {
	project := uniqueSlug()
	list := uniqueSlug()
	group := func(grants string) string {
		return componentConfig(project, "a", "api", "") + fmt.Sprintf(`
resource "weblate_group" "test" {
  name = %q
  %s
}`, project, grants)
	}
	manual := group(`
  roles      = ["Review strings"]
  languages  = ["de"]
  projects   = [weblate_project.test.slug]
  components = [weblate_component.test.id]`)
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccClient(t)
			createTestComponentList(t, list)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project),
		Steps: []resource.TestStep{
			{
				Config: manual,
				ConfigStateChecks: []statecheck.StateCheck{
					expectGroup("roles", stringSetExact("Review strings")),
					expectGroup("languages", stringSetExact("de")),
					expectGroup("projects", stringSetExact(project)),
					expectGroup("component_lists", stringSetExact()),
					expectGroup("components", stringSetExact(project+"/a/api")),
				},
				// A Grant added outside the configuration shows as drift.
				Check:              addGrantOutside(t, client.LanguageGrants, "cs"),
				ExpectNonEmptyPlan: true,
			},
			{
				// The apply removes the Grant added outside the configuration.
				Config: manual,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(groupAddress, plancheck.ResourceActionUpdate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					expectGroup("languages", stringSetExact("de")),
				},
			},
			{
				Config: group(fmt.Sprintf(`
  project_selection = "component_list"
  component_lists   = [%q]
  components        = []`, list)),
				ConfigStateChecks: []statecheck.StateCheck{
					expectGroup("component_lists", stringSetExact(list)),
					expectGroup("components", stringSetExact()),
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

func TestAccGroupResourceOmittedGrants(t *testing.T) {
	name := uniqueSlug()
	config := func(attributes string) string {
		return fmt.Sprintf(`
resource "weblate_project" "test" {
  name = %[1]q
  slug = %[1]q
  web  = "https://example.com"
}

resource "weblate_group" "test" {
  name = %[1]q
  %[2]s
}`, name, attributes)
	}
	all := config(`project_selection = "all"`)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, name),
		Steps: []resource.TestStep{
			{
				Config: all,
				Check:  addGrantOutside(t, client.LanguageGrants, "de"),
			},
			{
				// languages is omitted, so the Language added outside the configuration is no drift.
				Config: all,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					expectGroup("languages", stringSetExact("de")),
				},
			},
			{
				// The PATCH of project_selection goes first, so Weblate keeps the Project added after it.
				Config: config(`
  project_selection = "manual"
  projects          = [weblate_project.test.slug]`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectGroup("project_selection", knownvalue.StringExact("manual")),
					expectGroup("projects", stringSetExact(name)),
					expectGroup("languages", stringSetExact("de")),
				},
			},
			{
				// Weblate clears the Projects when project_selection leaves manual.
				Config: all,
				ConfigStateChecks: []statecheck.StateCheck{
					expectGroup("projects", stringSetExact()),
				},
			},
			{
				Config:      config(`project_selection = "all"` + "\n" + `projects = []`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Projects that the Group does not use`),
			},
		},
	})
}

func TestAccGroupResourceProjectScopedGrants(t *testing.T) {
	slug := uniqueSlug()
	config := func(attributes string) string {
		return fmt.Sprintf(`
resource "weblate_project" "test" {
  name = %[1]q
  slug = %[1]q
  web  = "https://example.com"
}

resource "weblate_group" "test" {
  name    = %[1]q
  project = weblate_project.test.slug
  %[2]s
}`, slug, attributes)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, slug),
		Steps: []resource.TestStep{
			{
				Config: config(`roles = ["Review strings"]`),
				ConfigStateChecks: []statecheck.StateCheck{
					expectGroup("roles", stringSetExact("Review strings")),
					// Weblate grants the Group its own Project.
					expectGroup("projects", stringSetExact(slug)),
				},
			},
			{
				Config:      config(`projects = [weblate_project.test.slug]`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Projects on a Group scoped to a Project`),
			},
		},
	})
}
