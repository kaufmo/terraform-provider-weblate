package provider

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

func expectCategory(name, attribute string, value knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue("weblate_category."+name, tfjsonpath.New(attribute), value)
}

func expectCategoryUpdate(name string) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction("weblate_category."+name, plancheck.ResourceActionUpdate),
	}}
}

// category is one weblate_category of categoriesConfig: the resource name, the resource name of the parent ("" for
// the Project root) and the slug.
type category struct{ name, parent, slug string }

// categoriesConfig returns a Project with the given slug and the given Categories in it.
func categoriesConfig(project string, categories []category) string {
	config := fmt.Sprintf(`
resource "weblate_project" "test" {
  name = %[1]q
  slug = %[1]q
  web  = "https://example.com"
}`, project)
	for _, c := range categories {
		parent := ""
		if c.parent != "" {
			parent = fmt.Sprintf("category = weblate_category.%s.id", c.parent)
		}
		config += fmt.Sprintf(`
resource "weblate_category" %[1]q {
  project = weblate_project.test.slug
  %[2]s
  name    = "Category %[3]s"
  slug    = %[3]q
}`, c.name, parent, c.slug)
	}
	return config
}

func TestAccCategoryResource(t *testing.T) {
	project := uniqueSlug()
	sameWeblateID := statecheck.CompareValue(compare.ValuesSame())
	var deletedID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project),
		Steps: []resource.TestStep{
			{
				Config: categoriesConfig(project, []category{{"a", "", "a"}, {"b", "a", "b"}, {"c", "b", "c"}}),
				ConfigStateChecks: []statecheck.StateCheck{
					sameWeblateID.AddStateValue("weblate_category.c", tfjsonpath.New("weblate_id")),
					expectCategory("a", "id", knownvalue.StringExact(project+"/a")),
					expectCategory("a", "category", knownvalue.Null()),
					expectCategory("b", "id", knownvalue.StringExact(project+"/a/b")),
					expectCategory("c", "id", knownvalue.StringExact(project+"/a/b/c")),
					expectCategory("c", "category", knownvalue.StringExact(project+"/a/b")),
				},
			},
			{
				Config:      categoriesConfig(project, []category{{"a", "", "a"}, {"b", "a", "b"}, {"c", "b", "c"}, {"d", "c", "d"}}),
				ExpectError: regexp.MustCompile(`Too\s+deep\s+nesting`),
			},
			{
				Config:           categoriesConfig(project, []category{{"a", "", "a"}, {"b", "a", "b"}, {"c", "b", "renamed"}}),
				ConfigPlanChecks: expectCategoryUpdate("c"),
				ConfigStateChecks: []statecheck.StateCheck{
					sameWeblateID.AddStateValue("weblate_category.c", tfjsonpath.New("weblate_id")),
					expectCategory("c", "id", knownvalue.StringExact(project+"/a/b/renamed")),
					expectCategory("c", "name", knownvalue.StringExact("Category renamed")),
				},
			},
			{
				Config:           categoriesConfig(project, []category{{"a", "", "a"}, {"b", "a", "b"}, {"c", "a", "renamed"}}),
				ConfigPlanChecks: expectCategoryUpdate("c"),
				ConfigStateChecks: []statecheck.StateCheck{
					sameWeblateID.AddStateValue("weblate_category.c", tfjsonpath.New("weblate_id")),
					expectCategory("c", "id", knownvalue.StringExact(project+"/a/renamed")),
				},
			},
			{
				Config:           categoriesConfig(project, []category{{"a", "", "a"}, {"b", "a", "b"}, {"c", "", "renamed"}}),
				ConfigPlanChecks: expectCategoryUpdate("c"),
				ConfigStateChecks: []statecheck.StateCheck{
					sameWeblateID.AddStateValue("weblate_category.c", tfjsonpath.New("weblate_id")),
					expectCategory("c", "id", knownvalue.StringExact(project+"/renamed")),
					expectCategory("c", "category", knownvalue.Null()),
				},
				Check: resource.TestCheckResourceAttrWith("weblate_category.b", "weblate_id", func(id string) error {
					deletedID = id
					return nil
				}),
			},
			{
				ResourceName:            "weblate_category.b",
				ImportState:             true,
				ImportStateId:           project + "/a/b",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				// Removing b deletes it; right after the apply, Weblate must answer 404 for it.
				Config: categoriesConfig(project, []category{{"a", "", "a"}, {"c", "", "renamed"}}),
				Check: func(*terraform.State) error {
					id, err := strconv.ParseInt(deletedID, 10, 64)
					if err != nil {
						return err
					}
					if _, err := testAccClient(t).GetCategory(context.Background(), id); !client.IsNotFound(err) {
						return fmt.Errorf("category %d still exists after delete: %v", id, err)
					}
					return nil
				},
			},
		},
	})
}

func TestAccCategoryResourceProjectChange(t *testing.T) {
	project := uniqueSlug()
	renamed := project + "-renamed"
	other := project + "-other"
	sameWeblateID := statecheck.CompareValue(compare.ValuesSame())
	categoryIn := func(projectSlug, categoryProject string) string {
		return fmt.Sprintf(`
resource "weblate_project" "test" {
  name = %[1]q
  slug = %[1]q
  web  = "https://example.com"
}

resource "weblate_category" "test" {
  project = %[2]s
  name    = "Guides"
  slug    = "guides"
}`, projectSlug, categoryProject)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { createTestProject(t, other) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project, renamed),
		Steps: []resource.TestStep{
			{
				Config: categoryIn(project, "weblate_project.test.slug"),
				ConfigStateChecks: []statecheck.StateCheck{
					sameWeblateID.AddStateValue("weblate_category.test", tfjsonpath.New("weblate_id")),
				},
			},
			{
				// A Project rename moves its Categories along; the Category only updates its state.
				Config:           categoryIn(renamed, "weblate_project.test.slug"),
				ConfigPlanChecks: expectCategoryUpdate("test"),
				ConfigStateChecks: []statecheck.StateCheck{
					sameWeblateID.AddStateValue("weblate_category.test", tfjsonpath.New("weblate_id")),
					expectCategory("test", "id", knownvalue.StringExact(renamed+"/guides")),
				},
			},
			{
				Config:      categoryIn(renamed, fmt.Sprintf("%q", other)),
				ExpectError: regexp.MustCompile(`-replace`),
			},
		},
	})
}
