package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
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

func TestAccComponentResourceLinked(t *testing.T) {
	project := uniqueSlug()
	config := componentConfig(project, "", "test", "") + `
resource "weblate_component" "linked" {
  project     = weblate_project.test.slug
  name        = "Component linked"
  slug        = "linked"
  repo        = "weblate://${weblate_component.test.id}"
  filemask    = "po/*.po"
  new_base    = "po/messages.pot"
  file_format = "po"
}

# The same repository as weblate_component.test, which create does not turn into a link.
resource "weblate_component" "unlinked" {
  project     = weblate_project.test.slug
  name        = "Component unlinked"
  slug        = "unlinked"
  repo        = "http://git.test.example/demo.git"
  filemask    = "po/*.po"
  new_base    = "po/messages.pot"
  file_format = "po"
  depends_on  = [weblate_component.test]
}`
	expect := func(name, attribute string, value knownvalue.Check) statecheck.StateCheck {
		return statecheck.ExpectKnownValue("weblate_component."+name, tfjsonpath.New(attribute), value)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project),
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					expect("linked", "linked_component", knownvalue.StringExact(project+"/test")),
					expect("linked", "repo", knownvalue.StringExact("weblate://"+project+"/test")),
					// Weblate returns the branch of the linked Component.
					expect("linked", "branch", knownvalue.StringExact("main")),
					expect("unlinked", "linked_component", knownvalue.Null()),
					expect("unlinked", "repo", knownvalue.StringExact("http://git.test.example/demo.git")),
					expect("test", "linked_component", knownvalue.Null()),
				},
			},
			{
				ResourceName:            "weblate_component.linked",
				ImportState:             true,
				ImportStateId:           project + "/linked",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}

// grantReviewStrings adds the user reviewer of test/weblate/start.sh to a new Group with the Role Review strings on
// all Projects. The Role lacks vcs.view, and Weblate's default Groups of the user grant nothing on private Projects.
func grantReviewStrings(t *testing.T) {
	t.Helper()
	c := testAccClient(t)
	if os.Getenv("WEBLATE_REVIEWER_API_TOKEN") == "" {
		t.Fatal("WEBLATE_REVIEWER_API_TOKEN must be set for acceptance tests; start Weblate with test/weblate/start.sh")
	}
	ctx := context.Background()
	roles, err := c.ListRoles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(roles, func(r client.Role) bool { return r.Name == "Review strings" })
	if i < 0 {
		t.Fatal("Weblate has no Role Review strings")
	}
	g, err := c.CreateGroup(ctx, map[string]any{"name": uniqueSlug(), "project_selection": 1, "language_selection": 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.DeleteGroup(ctx, g.ID); err != nil && !client.IsNotFound(err) {
			t.Errorf("deleting Group %d: %v", g.ID, err)
		}
	})
	for ref, body := range map[string]map[string]any{
		client.GroupRef(g.ID) + "roles/": {"role_id": roles[i].ID},
		"users/reviewer/groups/":         {"group_id": g.ID},
	} {
		if err := c.Do(ctx, http.MethodPost, ref, body, nil); err != nil {
			t.Fatalf("POST %s: %v", ref, err)
		}
	}
}

func TestAccComponentResourceWithoutVCSView(t *testing.T) {
	project := uniqueSlug()
	// Private, so that the default Group Users, with vcs.view on public Projects, grants nothing on it.
	config := strings.Replace(componentConfig(project, "", "test", `branch = "main"`),
		`web  = "https://example.com"`, "web  = \"https://example.com\"\n  access_control = 100", 1)
	asReviewer := fmt.Sprintf(`
provider "weblate" {
  token = %q
}
`, os.Getenv("WEBLATE_REVIEWER_API_TOKEN")) + config
	emptyPlan := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { grantReviewStrings(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkProjectsDestroyed(t, project),
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				// The refresh with the reviewer's token returns the warning and keeps the values from state.
				Config:           asReviewer,
				ConfigPlanChecks: emptyPlan,
				ConfigStateChecks: []statecheck.StateCheck{
					expectComponent("repo", knownvalue.StringExact("http://git.test.example/demo.git")),
					expectComponent("vcs", knownvalue.StringExact("git")),
				},
				Check: func(*terraform.State) error {
					c, err := client.New(os.Getenv("WEBLATE_URL"), os.Getenv("WEBLATE_REVIEWER_API_TOKEN"))
					if err != nil {
						return err
					}
					comp, err := c.GetComponent(context.Background(), client.ComponentRef(project+"/test"))
					if err == nil && string(comp.Fields["vcs"]) != "null" {
						err = fmt.Errorf("weblate returns vcs %s to the reviewer, want null", comp.Fields["vcs"])
					}
					return err
				},
			},
			{
				// Back to the admin token, which the destroy needs.
				Config:           config,
				ConfigPlanChecks: emptyPlan,
			},
		},
	})
}
