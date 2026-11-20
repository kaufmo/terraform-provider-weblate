package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccProjectDataSource(t *testing.T) {
	slug := uniqueSlug()
	expect := func(attribute string, value knownvalue.Check) statecheck.StateCheck {
		return statecheck.ExpectKnownValue("data.weblate_project.test", tfjsonpath.New(attribute), value)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { createTestProject(t, slug) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`data "weblate_project" "test" { slug = %q }`, slug),
			ConfigStateChecks: []statecheck.StateCheck{
				expect("id", knownvalue.StringExact(slug)),
				expect("weblate_id", knownvalue.NotNull()),
				expect("name", knownvalue.StringExact(slug)),
				expect("web", knownvalue.StringExact("https://example.com")),
				expect("workspace", knownvalue.Null()),
				expect("access_control", knownvalue.Int64Exact(0)),
				expect("commit_policy", knownvalue.Int64Exact(0)),
				expect("enable_hooks", knownvalue.Bool(true)),
			},
		}},
	})
}

func TestAccProjectDataSourceMissingProject(t *testing.T) {
	slug := uniqueSlug()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      fmt.Sprintf(`data "weblate_project" "test" { slug = %q }`, slug),
			ExpectError: regexp.MustCompile(`Project not found(.|\n)*` + regexp.QuoteMeta(slug)),
		}},
	})
}
