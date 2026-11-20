resource "weblate_component" "app" {
  project                  = weblate_project.docs.slug
  category                 = weblate_category.guides.id
  name                     = "App"
  slug                     = "app"
  repo                     = "https://github.com/example/app.git"
  repo_username            = "x-access-token"
  repo_password_wo         = var.github_token
  repo_password_wo_version = 1
  branch                   = "main"
  filemask                 = "po/*.po"
  new_base                 = "po/messages.pot"
  file_format              = "po"

  file_format_params = jsonencode({
    po_line_wrap = -1
  })
}
