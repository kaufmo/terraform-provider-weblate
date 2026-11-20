# An Inherited setting either sets a value or inherits, never both

Each of the 12 Inherited settings (`license`, `agreement`, `new_lang`, `language_code_style`, `secondary_language`,
`enforced_checks`, the six `*_message` templates) is a pair of Optional and Computed attributes on `weblate_project`,
`weblate_category` and `weblate_component`: `<field>` and `inherit_<field>`. Setting both, with
`inherit_<field> = true`, is a plan-time error. With a value set, the provider sends the value together with
`inherit_<field> = false` (see the amendment below). With `inherit_<field> = true`, the provider sends only the flag.
With both omitted, the setting is not managed.

Weblate sets `inherit_<field>` to `false` whenever an update changes `<field>`, even when the same request sends
`inherit_<field> = true`. A configuration with both would therefore never converge, and while inheritance is on,
Weblate ignores the stored value anyway.

On `weblate_project`, `inherit_<field> = true` is a plan-time error when `workspace` is known to be `null`: a Project
inherits only from a Workspace, and Weblate stores the flag without an effect.

## Considered options

- **Only `<field>`, with `null` meaning inherit**: no extra attributes, but an omitted attribute could no longer mean
  "not managed", which every other Optional and Computed attribute of the provider means.
- **Expose `effective_<field>` as well**: rejected for v1.0. The resolved values change whenever a parent changes, so
  they would show up as changes outside OpenTofu on every refresh. They can be added later without a breaking change.

## Consequences

- With inheritance on, state still holds the stored value that Weblate returns for `<field>`, usually the model
  default. It has no effect.

## Amendment: send `inherit_<field> = false` with the value

The first version of this decision sent only the value and relied on Weblate to clear `inherit_<field>`. Weblate
`2026.10.1` clears the flag only when the stored value changes (`should_disable_inheritance` in
`weblate/trans/inherited_settings.py`). After `inherit_<field> = true`, the stored value stays as it was, so switching
back to that same value changed nothing, and the setting stayed inherited without an error. The same happened when a
setting that had inherited since Create got a value equal to the default Weblate stored then, such as `new_lang =
"add"`. The serializer accepts `inherit_<field> = false` next to the value, and `save()` does not set it back
([#31](https://github.com/kaufmo/terraform-provider-weblate/issues/31)).
