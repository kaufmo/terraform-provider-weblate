# terraform-provider-weblate
OpenTofu/Terraform provider for managing Weblate projects, components, add-ons and access control as code

## Requirements

- OpenTofu 1.11 or later, or Terraform 1.11 or later.
- Weblate `2026.10.1` or later.
- Go 1.26 or later to build the provider.

## Acceptance tests

The acceptance tests run against a real Weblate instance in Docker, next to a static git server that serves the
repository `http://git.test.example/demo.git`. They need Docker, Go, and OpenTofu or Terraform.

```sh
# Starts a fresh Weblate on port 8080 and exports WEBLATE_URL and WEBLATE_API_TOKEN.
export $(test/weblate/start.sh)
TF_ACC=1 go test ./... -run '^TestAcc' -v
docker compose -f test/weblate/compose.yaml down -v
```

- `start.sh` takes the Weblate image from `test/weblate/Dockerfile`. Set `WEBLATE_IMAGE`, such as
  `weblate/weblate:2026.10.1.4`, to test another version, and `WEBLATE_PORT` to use another port.
- To run the tests with OpenTofu, also set `TF_ACC_TERRAFORM_PATH=$(command -v tofu)`,
  `TF_ACC_PROVIDER_NAMESPACE=hashicorp` and `TF_ACC_PROVIDER_HOST=registry.opentofu.org`.
- `TestAccProjectResourceWorkspace` runs only with `WEBLATE_TEST_WORKSPACE` set to the UUID of a Workspace of the
  instance.
- Test objects have slugs that start with `tf-acc-`. To delete the objects that a canceled run left behind, run
  `go test ./internal/provider -v -sweep=all` with the same environment.
