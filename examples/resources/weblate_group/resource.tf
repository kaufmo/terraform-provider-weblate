resource "weblate_group" "reviewers" {
  name               = "Reviewers"
  project_selection  = "all_public"
  language_selection = "all"
}

# A Group scoped to the Project with the slug docs.
resource "weblate_group" "docs_translators" {
  name    = "Translators"
  project = "docs"
}
