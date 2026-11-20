# Add-on Create adopts an Add-on that is already installed

When Weblate rejects the `POST` of a `weblate_addon` with `400` and `Add-on already installed: <name>`, Create looks up
the Add-on with that name on the same Component or Project, writes it to state, and sends `configuration` with a
`PATCH` when the configuration sets it. Weblate installs Add-ons on its own when it creates a Component (the file
format's `autoaddon` set and the instance's `DEFAULT_ADDONS`). Without adoption, configuring one of them would need two
applies, because an `import` block needs the Component to exist at plan time.

Weblate decides whether an Add-on can be installed more than once (`multiple`), and the API does not expose that. An
Add-on that can be installed more than once never gets this `400`, so Create always installs a new one and never adopts.

## Considered options

- **Check the installed Add-ons by name before the `POST`**: needs a list in the provider of the Add-ons that can be
  installed more than once. Every new Add-on in Weblate would then need a provider release, which ADR 0004 avoids.
- **No adoption**: the error points to `import`, and auto-installed Add-ons need a second apply.

## Consequences

- The provider matches Weblate's English error text, which has no error code of its own. If the text changes, Create
  fails with Weblate's error, and the user imports the Add-on.
- Two `weblate_addon` resources with the same Add-on name on the same Component or Project adopt the same Add-on and
  overwrite each other's `configuration`. The provider does not detect this; the docs warn about it.
