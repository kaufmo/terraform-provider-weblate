# Resources never force replacement

`weblate_project`, `weblate_category` and `weblate_component` reference their parents by slug path, and no attribute
change plans a replacement. Replacing a Component deletes its Content (suggestions, comments, history), and Weblate
moves Categories and Components with their Project when the Project slug changes, so a `RequiresReplace` on `project`
would delete every Component of a renamed Project. The provider plans a `project` change as an in-place update and
compares numeric IDs on apply: the same Project ID means a rename and only updates state; a different ID fails with a
diagnostic that points to `-replace`, because the API cannot move objects between Projects.

## Considered options

- **Reference parents by numeric ID** (`project_id = weblate_project.docs.weblate_id`): a rename keeps the ID, so
  `RequiresReplace` would only fire on a real move. Rejected because the API reads Projects and Components only by
  slug path, so the provider would still need the slugs, and a numeric ID means nothing to the reader of a plan.

## Consequences

- `id` is the slug path and changes on rename or move. The numeric `weblate_id` is kept in state, and `Read` falls
  back to finding the object by `weblate_id` when its slug path returns `404`, so a rename in the Weblate UI shows up
  as drift instead of as a deleted object.
