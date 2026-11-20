# Secrets are write-only attributes

Every secret the provider sends to Weblate is a write-only attribute with an `int64` `*_wo_version` companion, so it
never reaches plan or state. Weblate has no write-only fields: it returns repository URLs without credentials and Add-on
secrets in plain text, so neither can be compared with the configuration. A secret change shows in the plan only when
the user bumps its `*_wo_version`. The provider cannot detect secrets that someone changed outside OpenTofu.

**Repository credentials.** `repo` and `push` are URLs without credentials; a URL with userinfo fails at plan time.
`repo_username` is a plain attribute, `repo_password_wo` is write-only, and the same applies to `push_*`. The provider
builds the URL with `url.UserPassword`, so special characters need no escaping, and a token login uses the user name
that the host expects, such as `x-access-token` or `oauth2`. A `PATCH` that sets `repo` or `push` without credentials
deletes the stored ones, so Update sends `repo` only when `repo`, `repo_username` or `repo_password_wo_version` changes,
and then always with the credentials from the configuration; the same applies to `push`. On read, the provider removes
userinfo from both URLs itself, for instances that set `HIDE_REPO_CREDENTIALS = False`.

**Add-on secrets.** `secret_configuration_wo` is a JSON string that the provider merges into `configuration` on every
write that sends `configuration`. A key in both is a plan-time error. `Read` has no access to the configuration, so the
provider removes these keys from the read-back `configuration` before the ADR 0004 read-back check and before writing
state: the key names recorded in private state at the last write, plus a built-in list of known secret keys (today only
the webhook Add-on's `secret`), so an import does not write a known secret into state either.

## Considered options

- **`sensitive` attributes**: still stored in plain text in state and plan.
- **One write-only URL with the credentials in it**: the URL itself would never show a diff.
- **A raw `user:token` write-only string**: users would have to percent-encode it themselves.
- **A hash of the secret in private state instead of `*_wo_version`**: it cannot produce a visible plan diff without
  marking a computed attribute unknown.
- **Only a built-in list of secret keys per Add-on**: a new secret field in Weblate would need a provider release,
  against ADR 0004.
- **Only key names in private state**: an import would write the webhook `secret` into state.

## Consequences

- After an import, or when credentials were set in the Weblate UI, the first change to `repo` without
  `repo_password_wo` deletes the stored credentials. The docs warn about this.
- SSH repository URLs need no secret in the provider: Weblate's SSH key is a server setting without an API.
- Translation service configuration, once it has a resource, follows the Add-on pattern.
