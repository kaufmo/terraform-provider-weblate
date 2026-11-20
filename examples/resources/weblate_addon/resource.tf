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

# The webhook secret never reaches the state. To send a new secret, change the version.
resource "weblate_addon" "webhook" {
  project = "docs"
  name    = "weblate.webhook.webhook"

  configuration = jsonencode({
    webhook_url  = "https://hooks.example.com/weblate"
    event_filter = "all"
    events       = []
  })

  secret_configuration_wo         = jsonencode({ secret = var.webhook_secret })
  secret_configuration_wo_version = 1
}
