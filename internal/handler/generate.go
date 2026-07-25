// Package handler serves the HTTP contract described by api/openapi.yaml.
package handler

//go:generate npx --no-install redocly build-docs ../../api/openapi.yaml --output documentation.gen.html --title "fizzbuzz API reference" --disableGoogleFont
//go:generate go tool oapi-codegen --config=oapi-codegen.yaml ../../api/openapi.yaml
