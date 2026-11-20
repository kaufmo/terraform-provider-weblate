# Issue tracker: GitHub

Issues and specs for this repo live as GitHub issues in `kaufmo/terraform-provider-weblate`. Use the tools of the `github` MCP server configured in `.mcp.json` for all operations, not the `gh` CLI. Pass `owner: kaufmo` and `repo: terraform-provider-weblate` on every call.

## Conventions

- **Create an issue**: `issue_write` with `method: create`, `title`, `body`, and optional `labels`.
- **Read an issue**: `issue_read` with `method: get`; use `method: get_comments` for the comments.
- **List issues**: `list_issues` with `state` and `labels` filters. Pass `fields` to leave out `body` when you only need numbers and titles.
- **Make an issue a sub-issue of a parent**: `issue_write` with `method: create` and `parent_issue_number`, or `sub_issue_write` with `method: add` afterwards. `sub_issue_id` takes the child's numeric database id (the `id` from `issue_read`), not its `#number`.
- **Blocking**: the MCP server has no tool for GitHub's native issue dependencies. Put a `Blocked by: #<n>, #<n>` line at the top of the child body. A ticket is unblocked when every listed blocker is closed.
- **Comment on an issue**: `add_issue_comment`.
- **Apply / remove labels**: `issue_write` with `method: update` and the full `labels` list. The list replaces the existing labels, so read them first with `issue_read` `method: get_labels`.
- **Close**: post the explanation with `add_issue_comment`, then `issue_write` with `method: update`, `state: closed` and a `state_reason`.

## Pull requests as a triage surface

**PRs as a request surface: no.** _(Set to `yes` if this repo treats external PRs as feature requests; `/triage` reads this flag.)_

When set to `yes`, PRs run through the same labels and states as issues: read them with `pull_request_read`, list them with `list_pull_requests`, and comment with `add_issue_comment` using the PR number.

GitHub shares one number space across issues and PRs, so a bare `#42` may be either: try `pull_request_read` and fall back to `issue_read`.

## When a skill says "publish to the issue tracker"

Create a GitHub issue.

## When a skill says "fetch the relevant ticket"

Read it as in **Read an issue** above.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a single issue with **child** issues as tickets.

- **Map**: a single issue labelled `wayfinder:map`, holding the Notes / Decisions-so-far / Fog body.
- **Child ticket**: an issue linked to the map as a GitHub sub-issue (see **Make an issue a sub-issue of a parent**). Labels: `wayfinder:<type>` (`research`/`prototype`/`grilling`/`task`). Once claimed, the ticket is assigned to the driving dev.
- **Blocking**: see **Blocking** under Conventions.
- **Frontier query**: list the map's open children (`issue_read` with `method: get_sub_issues`), drop any with an open issue in its `Blocked by` line or an assignee; first in map order wins.
- **Claim**: `issue_write` with `method: update` and `assignees`, the session's first write.
- **Resolve**: `add_issue_comment` with the answer, then close the issue, then append a context pointer (gist + link) to the map's Decisions-so-far.
