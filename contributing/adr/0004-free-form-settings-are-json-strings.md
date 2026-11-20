# Free-form settings are JSON strings

Add-on `configuration` and the Component fields `file_format_params` and `vcs_params` are JSON string attributes that
users set with `jsonencode(...)`, not typed attributes. Weblate defines about 64 Add-on settings in about 20 forms, and
they change between monthly releases, so typed attributes would tie each provider release to the Weblate versions it
was built for. The attributes use `jsontypes.Normalized`, so key order and whitespace cause no diff. They are Optional
and Computed: an omitted attribute is read into state and never changed.

Weblate stores only the keys it received, after its form validation. It drops unknown keys without an error and can
change a value's type. After create and update, the provider compares the read-back value with the plan and fails the
apply with a diagnostic that names each dropped or changed key, so a misspelled key fails on apply instead of causing
a diff on every plan.

## Considered options

- **Typed attributes per Add-on** (one nested attribute or one resource type per Add-on): plan-time validation and
  docs per setting, but a new provider release for every changed form, and version-dependent schemas, which the
  provider avoids for the whole supported Weblate range.
- **A `Dynamic` attribute** (an HCL object without `jsonencode`): rejected because the framework converts the types of
  dynamic values, which makes diffs against Weblate's own type conversion harder to predict.

## Consequences

- The provider does not check Add-on names, setting keys or setting values at plan time. Weblate rejects them on apply.
- `enforced_checks` is not free-form: it is a `set(string)` of check names, also without plan-time validation.
