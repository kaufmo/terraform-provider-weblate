resource "weblate_addon" "mo" {
  component = weblate_component.app.id
  name      = "weblate.gettext.mo"

  configuration = jsonencode({
    fuzzy = true
    path  = "{{ filename|stripext }}.mo"
  })
}

# An Add-on on the Project with the slug docs.
resource "weblate_addon" "consistency" {
  project = "docs"
  name    = "weblate.consistency.languages"
}
