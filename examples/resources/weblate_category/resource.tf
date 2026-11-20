resource "weblate_category" "guides" {
  project = weblate_project.docs.slug
  name    = "Guides"
  slug    = "guides"
}

resource "weblate_category" "api" {
  project  = weblate_project.docs.slug
  category = weblate_category.guides.id
  name     = "API reference"
  slug     = "api"
}
