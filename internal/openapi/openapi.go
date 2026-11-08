// Package openapi writes the OpenAPI 3.1 document of a JSON API from a list
// of operations. Request and response schemas are derived by reflection from
// the Go types of the bodies, reading their json struct tags, so the
// document describes exactly what the handlers encode and decode.
package openapi

import (
	"cmp"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Version is the OpenAPI version the document follows.
const Version = "3.1.0"

const jsonType = "application/json"

// Info describes the API as a whole.
type Info struct {
	Title       string
	Version     string
	Description string
	// Server is the URL every operation path is relative to.
	Server string
}

// Operation is one method on one path.
type Operation struct {
	Method string
	// Path is relative to the server, with parameters in braces.
	Path    string
	ID      string
	Summary string
	Tag     string
	// Secured operations require a personal API token.
	Secured bool
	Params  []Param
	// Request is an example request body, whose type gives its schema; nil
	// when the operation takes no JSON body.
	Request any
	// Form is a value of the type of a multipart form body, whose fields'
	// json tags name the form fields; nil when the operation takes none.
	Form      any
	Responses []Response
}

// File is a field or body that holds a file's bytes.
type File struct{}

const formType = "multipart/form-data"

// Param is a path or query parameter. Path parameters are required.
type Param struct {
	Name string
	// In is "path" or "query".
	In          string
	Description string
	// Example is a value the parameter may take; its type gives the schema.
	Example any
}

// Response is one status an operation may answer with.
type Response struct {
	Status      int
	Description string
	// Body is a value of the body's type, nil when the response has none.
	Body any
	// Type is the media type of a body that is not JSON, such as a File.
	Type string
}

// Schema is a JSON Schema as OpenAPI 3.1 uses it.
type Schema struct {
	Ref         string            `json:"$ref,omitempty"`
	Type        string            `json:"type,omitempty"`
	Format      string            `json:"format,omitempty"`
	Description string            `json:"description,omitempty"`
	Items       *Schema           `json:"items,omitempty"`
	Properties  map[string]Schema `json:"properties,omitempty"`
	Required    []string          `json:"required,omitempty"`
	// AdditionalProperties is the schema of every value of an object
	// whose keys are not fixed.
	AdditionalProperties *Schema `json:"additionalProperties,omitempty"`
}

type document struct {
	OpenAPI    string                          `json:"openapi"`
	Info       info                            `json:"info"`
	Servers    []server                        `json:"servers"`
	Paths      map[string]map[string]operation `json:"paths"`
	Components components                      `json:"components"`
}

type info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type server struct {
	URL string `json:"url"`
}

type operation struct {
	OperationID string                `json:"operationId"`
	Summary     string                `json:"summary"`
	Tags        []string              `json:"tags"`
	Security    []map[string][]string `json:"security"`
	Parameters  []parameter           `json:"parameters,omitempty"`
	RequestBody *requestBody          `json:"requestBody,omitempty"`
	Responses   map[string]response   `json:"responses"`
}

type parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Schema      Schema `json:"schema"`
	Example     any    `json:"example"`
}

type requestBody struct {
	Required bool                 `json:"required"`
	Content  map[string]mediaType `json:"content"`
}

type mediaType struct {
	Schema  Schema `json:"schema"`
	Example any    `json:"example,omitempty"`
}

type response struct {
	Description string               `json:"description"`
	Content     map[string]mediaType `json:"content,omitempty"`
}

type components struct {
	Schemas         map[string]Schema         `json:"schemas"`
	SecuritySchemes map[string]securityScheme `json:"securitySchemes"`
}

type securityScheme struct {
	Type        string `json:"type"`
	Scheme      string `json:"scheme"`
	Description string `json:"description"`
}

// The security schemes a secured operation accepts: either one carries a
// personal API token.
var securitySchemes = map[string]securityScheme{
	"bearerAuth": {Type: "http", Scheme: "bearer",
		Description: "A personal API token in the Authorization header: `Authorization: Bearer <token>`."},
	"basicAuth": {Type: "http", Scheme: "basic",
		Description: "A personal API token as the HTTP Basic password. The user name is ignored."},
}

var (
	tokenSecurity = []map[string][]string{{"bearerAuth": {}}, {"basicAuth": {}}}
	noSecurity    = []map[string][]string{}
)

// Document returns the OpenAPI document describing ops, as indented JSON.
func Document(about Info, ops []Operation) []byte {
	g := generator{schemas: map[string]Schema{}}
	paths := map[string]map[string]operation{}
	for _, op := range ops {
		if paths[op.Path] == nil {
			paths[op.Path] = map[string]operation{}
		}
		paths[op.Path][strings.ToLower(op.Method)] = g.operation(op)
	}
	doc := document{
		OpenAPI:    Version,
		Info:       info{Title: about.Title, Version: about.Version, Description: about.Description},
		Servers:    []server{{URL: about.Server}},
		Paths:      paths,
		Components: components{Schemas: g.schemas, SecuritySchemes: securitySchemes},
	}
	out, _ := json.MarshalIndent(doc, "", "  ")
	return append(out, '\n')
}

// generator collects the named schemas of the struct types it meets.
type generator struct {
	schemas map[string]Schema
}

func (g generator) operation(op Operation) operation {
	security := noSecurity
	if op.Secured {
		security = tokenSecurity
	}
	out := operation{
		OperationID: op.ID,
		Summary:     op.Summary,
		Tags:        []string{op.Tag},
		Security:    security,
		Responses:   map[string]response{},
	}
	for _, p := range op.Params {
		out.Parameters = append(out.Parameters, parameter{
			Name: p.Name, In: p.In, Required: p.In == "path", Description: p.Description,
			Schema: g.schema(reflect.TypeOf(p.Example)), Example: p.Example,
		})
	}
	if op.Request != nil {
		body := mediaType{Schema: g.schema(reflect.TypeOf(op.Request)), Example: op.Request}
		out.RequestBody = &requestBody{Required: true, Content: map[string]mediaType{jsonType: body}}
	}
	if op.Form != nil {
		form := mediaType{Schema: g.schema(reflect.TypeOf(op.Form))}
		out.RequestBody = &requestBody{Required: true, Content: map[string]mediaType{formType: form}}
	}
	for _, r := range op.Responses {
		out.Responses[strconv.Itoa(r.Status)] = g.response(r)
	}
	return out
}

func (g generator) response(r Response) response {
	out := response{Description: r.Description}
	if r.Body != nil {
		out.Content = map[string]mediaType{cmp.Or(r.Type, jsonType): {Schema: g.schema(reflect.TypeOf(r.Body))}}
	}
	return out
}

var (
	timeType = reflect.TypeFor[time.Time]()
	fileType = reflect.TypeFor[File]()
)

// scalarTypes maps the Go kinds a body field may have to JSON Schema types.
// A kind not listed gets a schema without a type, which accepts anything.
var scalarTypes = map[reflect.Kind]string{
	reflect.Bool:   "boolean",
	reflect.Int:    "integer",
	reflect.Int64:  "integer",
	reflect.String: "string",
}

// schema is the schema of values of t, as encoding/json writes them. A
// pointer is described by what it points to.
func (g generator) schema(t reflect.Type) Schema {
	if t == timeType {
		return Schema{Type: "string", Format: "date-time"}
	}
	if t == fileType {
		return Schema{Type: "string", Format: "binary"}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return g.schema(t.Elem())
	case reflect.Slice:
		items := g.schema(t.Elem())
		return Schema{Type: "array", Items: &items}
	case reflect.Map:
		values := g.schema(t.Elem())
		return Schema{Type: "object", AdditionalProperties: &values}
	case reflect.Struct:
		return g.ref(t)
	}
	return Schema{Type: scalarTypes[t.Kind()]}
}

// ref names the struct type t as a component schema, describing it the
// first time, and refers to it.
func (g generator) ref(t reflect.Type) Schema {
	name := strings.ToUpper(t.Name()[:1]) + t.Name()[1:]
	if _, done := g.schemas[name]; !done {
		g.schemas[name] = g.object(t)
	}
	return Schema{Ref: "#/components/schemas/" + name}
}

// object describes a struct by its exported fields' json tags. A field
// tagged omitempty may be absent; every other field is required.
func (g generator) object(t reflect.Type) Schema {
	out := Schema{Type: "object", Properties: map[string]Schema{}}
	for f := range t.Fields() {
		name, options, _ := strings.Cut(f.Tag.Get("json"), ",")
		property := g.schema(f.Type)
		property.Description = f.Tag.Get("doc")
		out.Properties[name] = property
		if !strings.Contains(options, "omitempty") {
			out.Required = append(out.Required, name)
		}
	}
	return out
}
