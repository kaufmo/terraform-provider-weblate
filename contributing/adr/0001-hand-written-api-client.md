# Hand-written API client instead of a generated one

The provider calls Weblate through a hand-written Go client in an internal package of this repository, although
Weblate publishes an OpenAPI 3.1 spec. v1.0 needs about 35 operations, and the spec is wrong where the provider
depends on it: the Group association endpoints document the wrong request body, fields that come back `null` are not
marked nullable, and blank-or-value fields such as `web` generate `json.RawMessage` unions. Generating with
`oapi-codegen` would add an overlay to maintain and tens of thousands of generated lines, and each field would still
need its own mapping into the resource schema. Acceptance tests against Weblate in Docker, not the spec, check that
the client matches the API across the supported Weblate versions.
