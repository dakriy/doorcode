package testsupport

import (
	"fmt"
	"maps"
	"math"
	"mime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Bodies are the response bodies an OpenAPI document describes, read once
// to check many responses against.
type Bodies struct {
	doc       openAPIDocument
	validator schemaValidator
}

// DocumentedBodies reads the response bodies an OpenAPI document describes.
func DocumentedBodies(document string) Bodies {
	schemas, _ := JSONAt(document, "/components/schemas").(map[string]any)
	return Bodies{doc: readOpenAPI(document), validator: schemaValidator{schemas: schemas}}
}

// Errors are the ways got's body departs from what the document says op
// answers with got's status: a body where none is documented, another
// media type, or JSON that its schema does not describe. A JSON value is
// described only when it has the schema's type, every required property,
// and no property the schema does not name. None when the body is as
// documented or the status is not listed.
func (b Bodies) Errors(op APIOperation, got Response) []string {
	content, listed := documentedContent(b.doc, op, got.Status)
	if !listed {
		return nil
	}
	if len(content) == 0 {
		if got.Body != "" {
			return []string{"a body where the document describes none"}
		}
		return nil
	}
	mediaType, _, _ := mime.ParseMediaType(got.ContentType)
	schema, documented := content[mediaType]
	if !documented {
		return []string{fmt.Sprintf("content type %q where the document describes %v", got.ContentType, slices.Sorted(maps.Keys(content)))}
	}
	if mediaType != "application/json" {
		return nil
	}
	return b.validator.check("", JSON(got.Body), schema)
}

// documentedContent is the schema of each media type the document gives
// op's response with status, and whether the status is listed at all.
func documentedContent(doc openAPIDocument, op APIOperation, status int) (map[string]any, bool) {
	for path, methods := range doc.Paths {
		entry, ok := methods[strings.ToLower(op.Method)]
		if !ok || doc.Servers[0].URL+exampleTarget(path, entry.Parameters) != op.Target {
			continue
		}
		response, listed := entry.Responses[strconv.Itoa(status)]
		content := map[string]any{}
		for mediaType, body := range response.Content {
			content[mediaType] = body.Schema
		}
		return content, listed
	}
	return nil, false
}

// schemaValidator checks JSON values against the schemas an OpenAPI
// document derives from wire types, resolving references to its
// component schemas.
type schemaValidator struct {
	schemas map[string]any
}

// check is the ways value, found at the JSON pointer at, departs from
// schema.
func (v schemaValidator) check(at string, value, schema any) []string {
	s, _ := schema.(map[string]any)
	if ref, ok := s["$ref"].(string); ok {
		return v.check(at, value, v.schemas[strings.TrimPrefix(ref, "#/components/schemas/")])
	}
	kind, _ := s["type"].(string)
	if kind == "" {
		return nil
	}
	if !hasType(value, kind) {
		return []string{fmt.Sprintf("%s: %s where the document describes %s", where(at), jsonKind(value), kind)}
	}
	switch kind {
	case "object":
		return v.checkObject(at, value.(map[string]any), s)
	case "array":
		var errs []string
		for i, item := range value.([]any) {
			errs = append(errs, v.check(at+"/"+strconv.Itoa(i), item, s["items"])...)
		}
		return errs
	case "string":
		return checkFormat(at, value.(string), s["format"])
	}
	return nil
}

// checkObject is the ways the object value departs from the object schema
// s: missing required properties, properties s does not name, and
// properties or values that depart from their own schemas.
func (v schemaValidator) checkObject(at string, value map[string]any, s map[string]any) []string {
	var errs []string
	properties, named := s["properties"].(map[string]any)
	required, _ := s["required"].([]any)
	for _, name := range required {
		if _, ok := value[name.(string)]; !ok {
			errs = append(errs, fmt.Sprintf("%s: no %q, which the document requires", where(at), name))
		}
	}
	values, open := s["additionalProperties"]
	for _, key := range slices.Sorted(maps.Keys(value)) {
		schema, known := properties[key]
		if !known && open {
			schema, known = values, true
		}
		if named && !known {
			errs = append(errs, fmt.Sprintf("%s: %q, which the document does not describe", where(at), key))
			continue
		}
		errs = append(errs, v.check(at+"/"+key, value[key], schema)...)
	}
	return errs
}

// hasType reports whether value, as JSON decodes it, is of the JSON
// Schema type kind.
func hasType(value any, kind string) bool {
	n, number := value.(float64)
	switch kind {
	case "integer":
		return number && n == math.Trunc(n)
	case "number":
		return number
	}
	return jsonKind(value) == kind
}

// jsonKind names the JSON Schema type of value as JSON decodes it.
func jsonKind(value any) string {
	switch value.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	}
	return "null"
}

// checkFormat is how s, at the JSON pointer at, departs from the string
// format the document gives it.
func checkFormat(at, s string, format any) []string {
	if format != "date-time" {
		return nil
	}
	if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
		return []string{fmt.Sprintf("%s: %q is not a date-time", where(at), s)}
	}
	return nil
}

// where names a JSON pointer for a message, the body itself when empty.
func where(at string) string {
	if at == "" {
		return "the body"
	}
	return at
}
