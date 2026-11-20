package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccRoleDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `data "weblate_role" "test" { name = "Administration" }`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.weblate_role.test", tfjsonpath.New("id"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue("data.weblate_role.test", tfjsonpath.New("permissions"),
					knownvalue.SetPartial([]knownvalue.Check{knownvalue.StringExact("project.edit")})),
			},
		}},
	})
}

func TestAccRoleDataSourceMissingRole(t *testing.T) {
	name := uniqueSlug()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccClient(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      `data "weblate_role" "test" { name = "` + name + `" }`,
			ExpectError: regexp.MustCompile(`Role not found(.|\n)*` + regexp.QuoteMeta(name)),
		}},
	})
}
