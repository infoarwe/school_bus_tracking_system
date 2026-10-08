// Package api embeds the OpenAPI spec that documents the HTTP API for the
// admin web and the mobile team. Update openapi.yaml in the same change as
// any endpoint change.
package api

import _ "embed"

//go:embed openapi.yaml
var OpenAPISpec []byte
