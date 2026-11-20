# Releasing

A release is a pushed `v*` tag. The `Release` workflow (`.github/workflows/release.yml`) then builds the provider with
GoReleaser, signs the `SHA256SUMS` file with the maintainer's GPG key and publishes a GitHub release. The Terraform
Registry and the OpenTofu Registry both read their versions from that release. The `Release check` workflow runs
`goreleaser check` and an unsigned snapshot build on every pull request, so a broken release config fails before a tag.

Version numbers follow SemVer, with the rules in ADR 0008.

## Cut a release

1. On `main`, rename the `CHANGELOG.md` heading `## X.Y.Z (Unreleased)` to the release date, for example
   `## 1.2.0 (March 4, 2027)`. Check that `X.Y.Z` matches the changes: a breaking change needs a new major version, a
   new resource or attribute a new minor version.
2. Commit the change as `chore: release vX.Y.Z` and push it.
3. Tag that commit and push the tag:

   ```sh
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

4. Wait for the `Release` workflow to finish. The release must have one zip per platform,
   `terraform-provider-weblate_X.Y.Z_manifest.json`, `terraform-provider-weblate_X.Y.Z_SHA256SUMS` and
   `terraform-provider-weblate_X.Y.Z_SHA256SUMS.sig`.
5. Add a new `## X.Y.Z (Unreleased)` section with the next expected version at the top of `CHANGELOG.md`.

Never delete a tag or replace the assets of a published release: both registries keep the checksums of a version they
have read. Fix a broken release with a new version.

A tag with a pre-release suffix, such as `v1.0.0-rc.1`, becomes a GitHub pre-release. Skip steps 1 and 5 for it.

## Pre-releases in the registries

The OpenTofu Registry ingests pre-release tags. Its scheduled job lists the repository's Git tags, not its GitHub
releases, and takes every tag that is valid SemVer (`src/internal/provider/build.go` in `opentofu/registry`, commit
`53b0415`). The GitHub pre-release flag does not matter. If the job runs before the release assets exist, it records an
error for that version and retries it later, waiting 30 minutes after the first error and twice as long after each
further one.

The Terraform Registry cannot publish a provider whose only releases are pre-releases: the first publish fails with
"release is a draft or pre-release". The first version there must be a full release.

Neither OpenTofu nor Terraform selects a pre-release for a range constraint such as `~> 1.0`. To check a pre-release,
pin it exactly and run `tofu init` and `terraform init` against an empty directory:

```hcl
terraform {
  required_providers {
    weblate = {
      source  = "kaufmo/weblate"
      version = "1.0.0-rc.1"
    }
  }
}
```

`init` downloads the provider and verifies the signature against the registered key. With OpenTofu, the source
`kaufmo/weblate` resolves to `registry.opentofu.org`; with Terraform, to `registry.terraform.io`.

## One-time maintainer steps

Do these before the first tag.

1. Create an RSA key. The Terraform Registry rejects the default ECC key type.

   ```sh
   gpg --full-generate-key  # choose "RSA and RSA", 4096 bits
   ```

   OpenTofu rejects an expired key when it is submitted. If the key has an expiry date, renew it and submit it again
   before it expires.
2. Add two repository secrets in GitHub under Settings > Secrets and variables > Actions:
   - `GPG_PRIVATE_KEY`: the output of `gpg --armor --export-secret-keys <KEY_ID>`
   - `PASSPHRASE`: the key's passphrase
3. Enable immutable releases under Settings > General > Releases. The OpenTofu Registry policy asks for it.
4. Register the public key (`gpg --armor --export <KEY_ID>`) in both registries:
   - Terraform Registry: User Settings > Signing Keys, namespace `kaufmo`.
   - OpenTofu Registry: the "Submit new Provider Signing Key" issue form in `opentofu/registry`, namespace `kaufmo`.
5. Publish the provider:
   - Terraform Registry: Publish > Provider, then select `kaufmo/terraform-provider-weblate`. This adds a webhook to
     the repository that reports each new release. If releases stop appearing, delete the `registry.terraform.io`
     webhooks and click Resync on the provider page.
   - OpenTofu Registry: the "Submit new Provider" issue form in `opentofu/registry`, with
     `kaufmo/terraform-provider-weblate`. After its pull request is merged, scheduled jobs pick up new tags; no webhook
     is needed.
