package api

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// The mobile team generates clients from this spec, so it must always be valid.
func TestOpenAPISpecIsValid(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(OpenAPISpec)
	if err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("invalid openapi.yaml: %v", err)
	}
}
