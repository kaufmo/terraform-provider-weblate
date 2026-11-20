## 1.0.0 (Unreleased)

NOTES:

* The provider is published to the OpenTofu and Terraform registries as `kaufmo/weblate`, with builds for Linux, macOS, Windows and FreeBSD and a GPG-signed `SHA256SUMS` file.

FEATURES:

* **New Provider:** `weblate` with the `url` and `token` arguments, which fall back to the `WEBLATE_URL` and `WEBLATE_API_TOKEN` environment variables.
* **New Data Source:** `weblate_project`, looked up by `slug`.
* **New Resource:** `weblate_project`, imported by slug. Renames happen in place, and delete waits until Weblate has removed the Project.
* **New Data Source:** `weblate_role`, looked up by `name`.
* **New Resource:** `weblate_category`, imported by slug path, such as `docs/guides`. Renames and moves happen in place, and delete waits until Weblate has removed the Category.
* **New Resource:** `weblate_component` with its own repository, imported by slug path, such as `docs/guides/app`. Renames and moves happen in place, create and update wait for Weblate's background task, and delete waits until Weblate has removed the Component. The credentials of `repo` and `push` are write-only attributes, `repo_password_wo` and `push_password_wo`, which never reach plan or state.
* **New Resource:** `weblate_group` without Grants, imported by numeric ID. A Group belongs to the whole instance or, with `project`, to one Project.
* **New Resource:** `weblate_addon` on a Component or a Project, imported by numeric ID. `configuration` is a JSON string, and create takes over an Add-on that Weblate has already installed.
