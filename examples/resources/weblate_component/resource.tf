resource "weblate_component" "app" {
  project     = weblate_project.docs.slug
  category    = weblate_category.guides.id
  name        = "App"
  slug        = "app"
  repo        = "https://github.com/example/app.git"
  branch      = "main"
  filemask    = "po/*.po"
  new_base    = "po/messages.pot"
  file_format = "po"

  file_format_params = jsonencode({
    po_line_wrap = -1
  })
}
