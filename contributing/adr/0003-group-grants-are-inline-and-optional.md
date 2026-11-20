# Group grants are inline attributes, and an omitted one is not managed

`weblate_group` holds its Grants as set attributes (`roles`, `projects`, `languages`, `components`,
`component_lists`) instead of one resource per Grant. Each attribute is Optional and Computed: when set, even to `[]`,
it is authoritative, and the provider removes Grants that were added in the Weblate UI; when omitted, the provider
reads the Grants into state and never plans a change to them. Omitted-means-unmanaged keeps the way open to add
Grant resources later, such as a `weblate_group_project` that a Project module uses to add its Project to a shared
Group, without a breaking change to `weblate_group`.

## Considered options

- **One resource per Grant type** (like `aws_iam_role_policy_attachment`): rejected for v0.1, because it adds five
  resource types, and the plan no longer shows a Group as one object. `project_selection = "all"` and
  `"component_list"` cover most cases where several modules add Projects to one Group.
- **Omitted means empty**: simpler, but a later Grant resource would fight with every `weblate_group` that omits the
  attribute.

## Consequences

- Removing an attribute from the configuration leaves the Grants in Weblate. To remove them, set the attribute to `[]`.
