package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
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

func expectComponent(attribute string, value knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue("weblate_component.test", tfjsonpath.New(attribute), value)
}

// componentConfig returns a Project with the Categories a and b and one Component from the test git server. category
// is the resource name of the Component's Category, or "" for the Project root; extra holds more attributes.
func componentConfig(project, category, slug, extra string) string {
	if category != "" {
		category = fmt.Sprintf("category = weblate_category.%s.id", category)
	}
	return fmt.Sprintf(`
resource "weblate_project" "test" {
  name = %[1]q
  slug = %[1]q
  web  = "https://example.com"
}

resource "weblate_category" "a" {
  project = weblate_project.test.slug
  name    = "Category a"
  slug    = "a"
}

resource "weblate_category" "b" {
  project = weblate_project.test.slug
  name    = "Category b"
  slug    = "b"
}

resource "weblate_component" "test" {
  project     = weblate_project.test.slug
  %[2]s
  name        = "Component %[3]s"
  slug        = %[3]q
  repo        = "http://git.test.example/demo.git"
  filemask    = "po/*.po"
  new_base    = "po/messages.pot"
  file_format = "po"
  %[4]s
}`, project, category, slug, extra)
}

func TestAccComponentResource(t *testing.T) {
	project := uniqueSlug()
	sameWeblateID := statecheck.CompareValue(compare.ValuesSame())
	// Not in jsonencode's sorted key order, and PostgreSQL stores JSON keys in an order of its own.
	params := `file_format_params = "{\"po_no_location\": true, \"po_keep_previous\": false}"`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project),
		Steps: []resource.TestStep{
			{
				// branch is omitted: Weblate takes it from HEAD of the repository, and the next plan is empty.
				Config: componentConfig(project, "", "test", params),
				ConfigStateChecks: []statecheck.StateCheck{
					sameWeblateID.AddStateValue("weblate_component.test", tfjsonpath.New("weblate_id")),
					expectComponent("id", knownvalue.StringExact(project+"/test")),
					expectComponent("category", knownvalue.Null()),
					expectComponent("branch", knownvalue.StringExact("main")),
					expectComponent("source_language", knownvalue.StringExact("en")),
				},
			},
			{
				Config: componentConfig(project, "a", "test", params),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("weblate_component.test", plancheck.ResourceActionUpdate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					sameWeblateID.AddStateValue("weblate_component.test", tfjsonpath.New("weblate_id")),
					expectComponent("id", knownvalue.StringExact(project+"/a/test")),
				},
			},
			{
				Config: componentConfig(project, "b", "renamed", params),
				ConfigStateChecks: []statecheck.StateCheck{
					sameWeblateID.AddStateValue("weblate_component.test", tfjsonpath.New("weblate_id")),
					expectComponent("id", knownvalue.StringExact(project+"/b/renamed")),
					expectComponent("name", knownvalue.StringExact("Component renamed")),
					expectComponent("category", knownvalue.StringExact(project+"/b")),
				},
			},
			{
				Config: componentConfig(project, "b", "renamed", params+"\n  priority = 80"),
				ConfigStateChecks: []statecheck.StateCheck{
					sameWeblateID.AddStateValue("weblate_component.test", tfjsonpath.New("weblate_id")),
					expectComponent("priority", knownvalue.Int64Exact(80)),
				},
			},
			{
				ResourceName:      "weblate_component.test",
				ImportState:       true,
				ImportStateId:     project + "/b/renamed",
				ImportStateVerify: true,
				// file_format_params keeps the configured spelling in state; the import reads Weblate's.
				ImportStateVerifyIgnore: []string{"timeouts", "file_format_params"},
			},
			{
				Config:      componentConfig(project, "b", "renamed", params+"\n  priority = 80\n  source_language = \"de\""),
				ExpectError: regexp.MustCompile(`-replace`),
			},
			{
				Config: strings.Replace(componentConfig(project, "b", "renamed", params),
					`"po/*.po"`, `"./po/*.po"`, 1),
				ExpectError: regexp.MustCompile(`"po/\*\.po"`),
			},
			{
				// Weblate rejects a parameter of another file format instead of dropping it.
				Config:      componentConfig(project, "b", "renamed", `file_format_params = jsonencode({ json_indent = 2 })`),
				ExpectError: regexp.MustCompile(`json_indent`),
			},
			{
				// Removing the Component deletes it; right after the apply, Weblate must answer 404 for it.
				Config: categoriesConfig(project, []category{{"a", "", "a"}, {"b", "", "b"}}),
				Check: func(*terraform.State) error {
					_, err := testAccClient(t).GetComponent(context.Background(), client.ComponentRef(project+"/b/renamed"))
					if !client.IsNotFound(err) {
						return fmt.Errorf("component %s/b/renamed still exists after delete: %v", project, err)
					}
					return nil
				},
			},
		},
	})
}

// The credentials of http://git.test.example/private/demo.git, from test/weblate/htpasswd.
const (
	testGitUsername = "weblate"
	testGitPassword = "acceptance-test-git-password"
)

// withoutText fails a plan or state check when the JSON of the plan or state contains it.
type withoutText string

func (s withoutText) check(v any) error {
	data, err := json.Marshal(v)
	if err == nil && bytes.Contains(data, []byte(s)) {
		err = fmt.Errorf("found %q", string(s))
	}
	return err
}

func (s withoutText) CheckPlan(_ context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	resp.Error = s.check(req.Plan)
}

func (s withoutText) CheckState(_ context.Context, req statecheck.CheckStateRequest, resp *statecheck.CheckStateResponse) {
	resp.Error = s.check(req.State)
}

func TestAccComponentResourceRepositoryCredentials(t *testing.T) {
	project := uniqueSlug()
	config := func(password string, version int, extra string) string {
		return strings.Replace(componentConfig(project, "", "test", fmt.Sprintf(`branch = "main"
  repo_username            = %q
  repo_password_wo         = %q
  repo_password_wo_version = %d
  %s`, testGitUsername, password, version, extra)), "/demo.git", "/private/demo.git", 1)
	}
	noSecrets := []plancheck.PlanCheck{withoutText(testGitPassword), withoutText("@git.test.example")}
	// A pull authenticates with the credentials that Weblate stored.
	pulls := func(*terraform.State) error {
		var result struct {
			Result bool `json:"result"`
		}
		err := testAccClient(t).Do(context.Background(), http.MethodPost,
			client.ComponentRef(project+"/test")+"repository/", map[string]string{"operation": "pull"}, &result)
		if err == nil && !result.Result {
			err = fmt.Errorf("weblate could not pull %s/test", project)
		}
		return err
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project),
		Steps: []resource.TestStep{
			{
				Config:           config(testGitPassword, 1, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: noSecrets},
				ConfigStateChecks: []statecheck.StateCheck{
					withoutText(testGitPassword), withoutText("@git.test.example"),
					expectComponent("repo", knownvalue.StringExact("http://git.test.example/private/demo.git")),
					expectComponent("repo_username", knownvalue.StringExact(testGitUsername)),
					expectComponent("repo_password_wo", knownvalue.Null()),
				},
				Check: pulls,
			},
			{
				// A PATCH without repo keeps the stored credentials.
				Config:           config(testGitPassword, 1, "priority = 80"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: noSecrets},
				Check:            pulls,
			},
			{
				// The new version sends repo with the wrong password, and Weblate's repository check fails.
				Config:      config("wrong-password", 2, "priority = 80"),
				ExpectError: regexp.MustCompile(`Could\s+not\s+update\s+repository`),
			},
			{
				Config:           config(testGitPassword, 3, "priority = 80"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: noSecrets},
				Check:            pulls,
			},
		},
	})
}

func TestAccComponentResourceUnreachableRepository(t *testing.T) {
	project := uniqueSlug()
	config := strings.Replace(componentConfig(project, "", "test", ""), "demo.git", "missing.git", 1)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`Could\s+not\s+update\s+repository`),
			},
		},
	})
}
