# Raising the minimum Weblate version is a minor release

From v1.0 on, the provider follows SemVer: a breaking change to the provider's schema or behavior needs a new major
version. Raising the minimum supported Weblate version is not counted as one. It ships in a minor release, with a
changelog entry under `BREAKING CHANGES` that names the new minimum.

Weblate releases monthly, changes its REST API without versioning it, and supports only recent releases. If each raise
of the minimum needed a major release, the provider would either release a major version every few months or keep
code for Weblate versions that Weblate itself no longer supports.

## Considered options

- **A raise is a major release**: strict SemVer for users who pin `~> 1.0`, at the cost of frequent major versions.
- **Version-dependent code for older Weblate releases**: rejected when the minimum was set; the provider has no
  version-dependent code.

## Consequences

- A user who pins `~> 1.0` can get a provider release that no longer supports their Weblate version. The changelog
  entry is the only warning.
