# Weblate Provider

An OpenTofu/Terraform provider that declares the configuration of a Weblate instance as code.

## Scope

**Configuration object**:
A Weblate object whose desired state an operator declares up front, such as a Project, Component, Add-on or Group.
_Avoid_: setting, entity

**Content**:
Data that translators and Weblate itself produce while working: units, suggestions, screenshots, translation memory, changes, reports. Never managed by the provider.
_Avoid_: translation data

**Weblate instance**:
One running Weblate server, identified by its base URL.
_Avoid_: server, site

## Weblate objects

**Project**:
The top-level container for Components, owned by one Weblate instance.

**Category**:
A folder inside a Project that groups Components; Categories can nest.

**Component**:
One translatable resource inside a Project, backed by one VCS repository and one file format.
_Avoid_: resource (clashes with the OpenTofu term)

**Slug path**:
The slugs from the Project down to a Category or Component, joined by `/`, such as `docs/guides/api`. Changes when any of these objects is renamed or moved.
_Avoid_: full slug, path

**Translation**:
One Language of one Component.

**Add-on**:
An automation attached to a Component or Project.
_Avoid_: addon, plugin

**Group**:
A set of users that holds Grants. A Group either belongs to the whole Weblate instance or is scoped to one Project.
_Avoid_: team

**Built-in Group**:
A Group that Weblate creates on its own for every Project, such as Administration or Review.
_Avoid_: default team, internal group

**Grant**:
One Role, Project, Language, Component or Component list that a Group gives its users access to.
_Avoid_: association, permission

**Role**:
A named set of permissions assigned to a Group.

## Credentials

**User token**:
A personal API token of one Weblate user, prefixed `wlu_`.

**Project token**:
An API token scoped to one Project, prefixed `wlp_`.
