resource "weblate_project" "docs" {
  name = "Documentation"
  slug = "docs"
  web  = "https://docs.example.com"

  instructions = <<-EOT
    Follow the style guide at https://docs.example.com/style.
  EOT

  translation_review = true
  commit_policy      = 30
  license            = "MIT"
}
