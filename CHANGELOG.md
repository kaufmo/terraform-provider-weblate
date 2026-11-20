## 1.0.0 (November 20, 2026)

NOTES:

* The provider is published to the OpenTofu and Terraform registries as `kaufmo/weblate`, with builds for Linux, macOS, Windows and FreeBSD and a GPG-signed `SHA256SUMS` file.

FEATURES:

* **New Provider:** `weblate` with the `url` and `token` arguments, which fall back to the `WEBLATE_URL` and `WEBLATE_API_TOKEN` environment variables.
* **New Data Source:** `weblate_project`, looked up by `slug`.
* **New Resource:** `weblate_project`, imported by slug. Renames happen in place, and delete waits until Weblate has removed the Project.
* **New Data Source:** `weblate_role`, looked up by `name`.
* **New Resource:** `weblate_category`, imported by slug path, such as `docs/guides`. Renames and moves happen in place, and delete waits until Weblate has removed the Category.
* **New Resource:** `weblate_component` with its own repository or as a Linked Component with `repo = "weblate://<id>"`, imported by slug path, such as `docs/guides/app`. Renames and moves happen in place, create and update wait for Weblate's background task, and delete waits until Weblate has removed the Component. The credentials of `repo` and `push` are write-only attributes, `repo_password_wo` and `push_password_wo`, which never reach plan or state. With a token that lacks `vcs.view`, Read warns and keeps the repository settings that Weblate hides from state.
* **New Resource:** `weblate_group`, imported by numeric ID. A Group belongs to the whole instance or, with `project`, to one Project; changing or removing `project` fails the apply. Its Grants are the set attributes `roles`, `languages`, `projects`, `component_lists` and `components`: a set attribute lists all Grants of its type, and an omitted one leaves the Grants as they are. A plan fails when a Grant attribute does not fit the selection modes or the Project scope.
* **New Resource:** `weblate_addon` on a Component or a Project, imported by numeric ID. `configuration` is a JSON string, secrets such as the webhook `secret` go into the write-only `secret_configuration_wo`, and create takes over an Add-on that Weblate has already installed.
* `weblate_project`, `weblate_category` and `weblate_component` manage the Inherited settings, such as `license` and the commit message templates, each with an `inherit_<setting>` attribute. The `weblate_project` data source reads them.
