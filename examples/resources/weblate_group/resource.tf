resource "weblate_group" "reviewers" {
  name               = "Reviewers"
  project_selection  = "all_public"
  language_selection = "all"
  roles              = ["Review strings"]
}

# A Group scoped to the Project with the slug docs. Weblate grants it the Project docs.
resource "weblate_group" "docs_translators" {
  name       = "Translators"
  project    = "docs"
  roles      = ["Power user"]
  languages  = ["de", "fr"]
  components = ["docs/guides/api"]
}

# A Group for the Components of the Component list with the slug core. languages is omitted, so Languages granted
# in the Weblate UI stay.
resource "weblate_group" "core" {
  name              = "Core translators"
  project_selection = "component_list"
  component_lists   = ["core"]
  roles             = ["Translate"]
}
