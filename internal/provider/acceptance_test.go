package provider

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/kaufmo/terraform-provider-weblate/internal/client"
)

// testAccPrefix starts the slug of every object that an acceptance test creates, so the sweepers find leftovers.
const testAccPrefix = "tf-acc-"

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"weblate": providerserver.NewProtocol6WithError(New("test")()),
}

// TestMain runs the sweepers instead of the tests when go test gets -sweep, such as
// `go test ./internal/provider -v -sweep=all`.
func TestMain(m *testing.M) {
	resource.TestMain(m)
}

func init() {
	resource.AddTestSweepers("weblate_project", &resource.Sweeper{
		Name: "weblate_project",
		F: func(string) error {
			c, err := client.New(os.Getenv("WEBLATE_URL"), os.Getenv("WEBLATE_API_TOKEN"))
			if err != nil {
				return err
			}
			return sweepProjects(context.Background(), c)
		},
	})
}

func sweepProjects(ctx context.Context, c *client.Client) error {
	projects, err := client.List[client.Project](ctx, c, "projects/")
	if err != nil {
		return err
	}
	var errs []error
	for _, p := range projects {
		if !strings.HasPrefix(p.Slug, testAccPrefix) {
			continue
		}
		if err := c.Do(ctx, http.MethodDelete, "projects/"+url.PathEscape(p.Slug)+"/", nil, nil); err != nil && !client.IsNotFound(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// uniqueSlug returns a slug that no other test run uses and that the sweepers recognize.
func uniqueSlug() string {
	return acctest.RandomWithPrefix(strings.TrimSuffix(testAccPrefix, "-"))
}

// testAccClient checks the environment of an acceptance test and returns a client for creating test fixtures. Call it
// from PreCheck, which runs only with TF_ACC set.
func testAccClient(t *testing.T) *client.Client {
	t.Helper()
	for _, env := range []string{"WEBLATE_URL", "WEBLATE_API_TOKEN"} {
		if os.Getenv(env) == "" {
			t.Fatalf("%s must be set for acceptance tests; start Weblate with test/weblate/start.sh", env)
		}
	}
	c, err := client.New(os.Getenv("WEBLATE_URL"), os.Getenv("WEBLATE_API_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// createTestProject creates a Project through the client, not through a resource, and deletes it when the test ends.
func createTestProject(t *testing.T, slug string) {
	t.Helper()
	c := testAccClient(t)
	ctx := context.Background()
	body := map[string]string{"name": slug, "slug": slug, "web": "https://example.com"}
	if err := c.Do(ctx, http.MethodPost, "projects/", body, nil); err != nil {
		t.Fatalf("creating Project %s: %v", slug, err)
	}
	t.Cleanup(func() {
		if err := c.Do(ctx, http.MethodDelete, "projects/"+slug+"/", nil, nil); err != nil && !client.IsNotFound(err) {
			t.Errorf("deleting Project %s: %v", slug, err)
		}
	})
}
