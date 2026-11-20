# AGENTS.md

## Agent skills

### Issue tracker

Issues live in GitHub Issues of `kaufmo/terraform-provider-weblate`, accessed through the `github` MCP server. See `contributing/agents/issue-tracker.md`.

### Triage labels

The five default labels: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `contributing/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` and `contributing/adr/` at the repo root. See `contributing/agents/domain.md`.

### Where docs live

`docs/` holds only the provider docs that `tfplugindocs` generates for the OpenTofu and Terraform registries; `tfplugindocs validate` rejects any other subdirectory there. Internal docs go to `contributing/`: agent configuration in `contributing/agents/`, ADRs in `contributing/adr/`, research findings in `contributing/research/`.
