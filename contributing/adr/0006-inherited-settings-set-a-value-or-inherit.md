# An Inherited setting either sets a value or inherits, never both

Each of the 12 Inherited settings (`license`, `agreement`, `new_lang`, `language_code_style`, `secondary_language`,
`enforced_checks`, the six `*_message` templates) is a pair of Optional and Computed attributes on `weblate_project`,
`weblate_category` and `weblate_component`: `<field>` and `inherit_<field>`. Setting both, with
`inherit_<field> = true`, is a plan-time error. With a value set, the provider sends only the value, and Weblate sets
`inherit_<field>` to `false` on its own. With `inherit_<field> = true`, the provider sends only the flag. With both
omitted, the setting is not managed.

Weblate sets `inherit_<field>` to `false` whenever an update changes `<field>`, even when the same request sends
`inherit_<field> = true`. A configuration with both would therefore never converge, and while inheritance is on,
Weblate ignores the stored value anyway.

On `weblate_project`, `inherit_<field> = true` is a plan-time error when `workspace` is known to be `null`: a Project
inherits only from a Workspace, and Weblate stores the flag without an effect.

## Considered options

- **Only `<field>`, with `null` meaning inherit**: no extra attributes, but an omitted attribute could no longer mean
  "not managed", which every other Optional and Computed attribute of the provider means.
- **Expose `effective_<field>` as well**: rejected for v0.1. The resolved values change whenever a parent changes, so
  they would show up as changes outside OpenTofu on every refresh. They can be added later without a breaking change.

## Consequences

- With inheritance on, state still holds the stored value that Weblate returns for `<field>`, usually the model
  default. It has no effect.
