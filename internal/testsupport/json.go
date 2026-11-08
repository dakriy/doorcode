package testsupport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// JSON decodes a JSON document into plain values (maps, slices, strings,
// float64 numbers, booleans, nil), so a test can compare it with cmp.Diff
// against JSON written by hand.
func JSON(document string) any {
	var v any
	if err := json.Unmarshal([]byte(document), &v); err != nil {
		panic(fmt.Sprintf("testsupport: not JSON: %v\n%s", err, document))
	}
	return v
}

// JSONAt is the value at a JSON pointer (RFC 6901) inside document, decoded
// as JSON decodes it; nil when nothing is there.
func JSONAt(document, pointer string) any {
	v := JSON(document)
	for _, token := range strings.Split(pointer, "/")[1:] {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		v = step(v, token)
	}
	return v
}

func step(v any, token string) any {
	switch node := v.(type) {
	case map[string]any:
		return node[token]
	case []any:
		i, err := strconv.Atoi(token)
		if err != nil || i < 0 || i >= len(node) {
			return nil
		}
		return node[i]
	}
	return nil
}

// JSONString is the string at a JSON pointer inside document, or "" when
// there is none.
func JSONString(document, pointer string) string {
	s, _ := JSONAt(document, pointer).(string)
	return s
}

// APIOperation is one operation an OpenAPI document lists, ready to call.
type APIOperation struct {
	Method string
	// Target is the server URL and path with every path parameter replaced
	// by its example.
	Target string
	ID     string
	// Body is the request body's example as JSON, empty without one.
	Body string
	// Statuses are the response statuses listed, in order.
	Statuses []int
}

type openAPIDocument struct {
	Servers []struct {
		URL string `json:"url"`
	} `json:"servers"`
	Paths map[string]map[string]struct {
		OperationID string `json:"operationId"`
		Parameters  []struct {
			Name    string `json:"name"`
			In      string `json:"in"`
			Example any    `json:"example"`
		} `json:"parameters"`
		RequestBody struct {
			Content map[string]struct {
				Example json.RawMessage `json:"example"`
			} `json:"content"`
		} `json:"requestBody"`
		Responses map[string]any `json:"responses"`
	} `json:"paths"`
}

// OpenAPIOperations reads every operation of an OpenAPI document, in path
// order and then method order.
func OpenAPIOperations(document string) []APIOperation {
	var doc openAPIDocument
	if err := json.Unmarshal([]byte(document), &doc); err != nil {
		panic(err)
	}
	var ops []APIOperation
	for _, path := range slices.Sorted(maps.Keys(doc.Paths)) {
		for _, method := range slices.Sorted(maps.Keys(doc.Paths[path])) {
			op := doc.Paths[path][method]
			target := path
			for _, p := range op.Parameters {
				target = strings.ReplaceAll(target, "{"+p.Name+"}", fmt.Sprint(p.Example))
			}
			var statuses []int
			for _, code := range slices.Sorted(maps.Keys(op.Responses)) {
				n, _ := strconv.Atoi(code)
				statuses = append(statuses, n)
			}
			ops = append(ops, APIOperation{
				Method:   strings.ToUpper(method),
				Target:   doc.Servers[0].URL + target,
				ID:       op.OperationID,
				Body:     compact(op.RequestBody.Content["application/json"].Example),
				Statuses: statuses,
			})
		}
	}
	return ops
}

// compact is raw JSON without insignificant whitespace, empty when raw is.
func compact(raw json.RawMessage) string {
	var out bytes.Buffer
	_ = json.Compact(&out, raw)
	return out.String()
}

// Target is the path and query of the absolute URL rawURL, which a test
// can request from the handler; empty when rawURL is not one.
func Target(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.RequestURI()
}
