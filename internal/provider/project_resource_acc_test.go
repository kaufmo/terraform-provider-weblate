package provider

import (
	"context"
	"fmt"
	"net/http"
	"os"
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

const projectAddress = "weblate_project.test"

func expectProject(attribute string, value knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(projectAddress, tfjsonpath.New(attribute), value)
}

func expectProjectAction(action plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(projectAddress, action)}}
}

// checkProjectsDestroyed checks that the Projects are gone right after destroy, which shows that Delete waited for
// Weblate's background task.
func checkProjectsDestroyed(t *testing.T, slugs ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, slug := range slugs {
			_, err := testAccClient(t).GetProject(context.Background(), slug)
			if err == nil {
				return fmt.Errorf("project %s still exists after destroy", slug)
			}
			if !client.IsNotFound(err) {
				return err
			}
		}
		return nil
	}
}

func TestAccProjectResource(t *testing.T) {
	slug := uniqueSlug()
	renamed := slug + "-renamed"
	outside := slug + "-outside"
	sameWeblateID := statecheck.CompareValue(compare.ValuesSame())

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, slug, renamed, outside),
		Steps: []resource.TestStep{
			{
				// A heredoc ends in a newline that Weblate trims; the test framework fails the step when the plan
				// after apply is not empty.
				Config: fmt.Sprintf(`
resource "weblate_project" "test" {
  name               = %[1]q
  slug               = %[1]q
  web                = "https://example.com"
  translation_review = true
  instructions       = <<-EOT
    Read the style guide.
  EOT
}`, slug),
				ConfigStateChecks: []statecheck.StateCheck{
					sameWeblateID.AddStateValue(projectAddress, tfjsonpath.New("weblate_id")),
					expectProject("id", knownvalue.StringExact(slug)),
					expectProject("weblate_id", knownvalue.NotNull()),
					expectProject("instructions", knownvalue.StringExact("Read the style guide.\n")),
					expectProject("translation_review", knownvalue.Bool(true)),
					expectProject("enable_hooks", knownvalue.Bool(true)),
					expectProject("workspace", knownvalue.Null()),
				},
			},
			{
				Config:           projectConfig(slug, "Renamed display name", `commit_policy = 30`),
				ConfigPlanChecks: expectProjectAction(plancheck.ResourceActionUpdate),
				ConfigStateChecks: []statecheck.StateCheck{
					expectProject("name", knownvalue.StringExact("Renamed display name")),
					expectProject("commit_policy", knownvalue.Int64Exact(30)),
					expectProject("instructions", knownvalue.StringExact("Read the style guide.")),
					expectProject("translation_review", knownvalue.Bool(true)),
				},
			},
			{
				ResourceName:            projectAddress,
				ImportState:             true,
				ImportStateId:           slug,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				Config:           projectConfig(renamed, "Renamed display name", `commit_policy = 30`),
				ConfigPlanChecks: expectProjectAction(plancheck.ResourceActionUpdate),
				ConfigStateChecks: []statecheck.StateCheck{
					sameWeblateID.AddStateValue(projectAddress, tfjsonpath.New("weblate_id")),
					expectProject("id", knownvalue.StringExact(renamed)),
				},
			},
			{
				// A rename in the Weblate UI: Read finds the Project by weblate_id and the plan renames it back.
				PreConfig: func() {
					body := map[string]string{"slug": outside}
					if err := testAccClient(t).Do(context.Background(), http.MethodPatch, client.ProjectRef(renamed), body, nil); err != nil {
						t.Fatalf("renaming Project %s outside OpenTofu: %v", renamed, err)
					}
				},
				Config:           projectConfig(renamed, "Renamed display name", `commit_policy = 30`),
				ConfigPlanChecks: expectProjectAction(plancheck.ResourceActionUpdate),
				ConfigStateChecks: []statecheck.StateCheck{
					sameWeblateID.AddStateValue(projectAddress, tfjsonpath.New("weblate_id")),
					expectProject("id", knownvalue.StringExact(renamed)),
				},
			},
		},
	})
}

// TestAccProjectResourceWorkspace needs the UUID of a Workspace of the test instance in WEBLATE_TEST_WORKSPACE.
func TestAccProjectResourceWorkspace(t *testing.T) {
	workspace := os.Getenv("WEBLATE_TEST_WORKSPACE")
	if workspace == "" {
		t.Skip("WEBLATE_TEST_WORKSPACE is not set: the Weblate instance has no Workspace for this test")
	}
	slug := uniqueSlug()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, slug),
		Steps: []resource.TestStep{{
			Config:            projectConfig(slug, slug, fmt.Sprintf("workspace = %q", workspace)),
			ConfigStateChecks: []statecheck.StateCheck{expectProject("workspace", knownvalue.StringExact(workspace))},
		}},
	})
}

func projectConfig(slug, name, extra string) string {
	return fmt.Sprintf(`
resource "weblate_project" "test" {
  name               = %q
  slug               = %q
  web                = "https://example.com"
  translation_review = true
  instructions       = "Read the style guide."
  %s
}`, name, slug, extra)
}
