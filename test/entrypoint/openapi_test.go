package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// openAPIDocument is the OpenAPI document a fresh application serves.
func openAPIDocument(t *testing.T) string {
	t.Helper()
	return testsupport.Get(start(t, testsupport.NewFactory()).Handler(), "/api/v1/openapi.json").Body
}

func TestServedOpenAPIDocumentIsTheGeneratedOne(t *testing.T) {
	got := testsupport.Get(start(t, testsupport.NewFactory()).Handler(), "/api/v1/openapi.json")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: string(app.OpenAPI())}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestOpenAPIListsEveryOperationWithTheStatusesItAnswers(t *testing.T) {
	got := testsupport.OpenAPIOperations(openAPIDocument(t))

	want := []testsupport.APIOperation{
		{Method: "GET", Target: "/api/v1/openapi.json", ID: "getOpenAPI", Statuses: []int{200}},
		{Method: "GET", Target: "/api/v1/settings", ID: "listSettings", Statuses: []int{200, 401, 403, 500}},
		{Method: "GET", Target: "/api/v1/settings/site_name", ID: "getSetting", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "PUT", Target: "/api/v1/settings/site_name", ID: "updateSetting", Body: `{"value":"North Campus"}`,
			Statuses: []int{200, 400, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/tokens", ID: "listTokens", Statuses: []int{200, 401, 500}},
		{Method: "POST", Target: "/api/v1/tokens", ID: "createToken", Body: `{"name":"deploy script","expires_in_days":90}`,
			Statuses: []int{201, 400, 401, 500}},
		{Method: "DELETE", Target: "/api/v1/tokens/1", ID: "revokeToken", Statuses: []int{204, 401, 404, 500}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("operations (-want +got):\n%s", diff)
	}
}

func TestOpenAPIDescribesTheAPIAndHowToAuthenticate(t *testing.T) {
	doc := openAPIDocument(t)

	got := []any{
		testsupport.JSONAt(doc, "/openapi"),
		testsupport.JSONAt(doc, "/info/title"),
		testsupport.JSONAt(doc, "/servers"),
		testsupport.JSONAt(doc, "/components/securitySchemes"),
		testsupport.JSONAt(doc, "/paths/~1openapi.json/get/security"),
		testsupport.JSONAt(doc, "/paths/~1tokens/get/security"),
	}

	want := testsupport.JSON(`["3.1.0", "doorcode API", [{"url": "/api/v1"}],
		{"bearerAuth": {"type": "http", "scheme": "bearer",
			"description": "A personal API token in the Authorization header: ` + "`Authorization: Bearer <token>`" + `."},
		 "basicAuth": {"type": "http", "scheme": "basic",
			"description": "A personal API token as the HTTP Basic password. The user name is ignored."}},
		[],
		[{"bearerAuth": []}, {"basicAuth": []}]]`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("document (-want +got):\n%s", diff)
	}
}

func TestOpenAPIDerivesBodySchemasFromTheWireTypes(t *testing.T) {
	doc := openAPIDocument(t)
	schemas := []string{"TokenView", "CreatedToken", "TokenRequest", "SettingList"}

	var got []any
	for _, name := range schemas {
		got = append(got, testsupport.JSONAt(doc, "/components/schemas/"+name))
	}

	want := testsupport.JSON(`[
		{"type": "object", "required": ["id", "name", "created_at", "expired"], "properties": {
			"id": {"type": "integer", "description": "Names the token, to revoke it."},
			"name": {"type": "string", "description": "What the token is for."},
			"created_at": {"type": "string", "format": "date-time", "description": "When the token was created."},
			"expires_at": {"type": "string", "format": "date-time", "description": "When the token stops working; absent when it never expires."},
			"expired": {"type": "boolean", "description": "True once the token has expired."}}},
		{"type": "object", "required": ["token", "secret"], "properties": {
			"token": {"$ref": "#/components/schemas/TokenView", "description": "The new token."},
			"secret": {"type": "string", "description": "The token to send. This is the only time it is shown; it cannot be recovered."}}},
		{"type": "object", "required": ["name"], "properties": {
			"name": {"type": "string", "description": "What the token is for, up to 100 characters."},
			"expires_in_days": {"type": "integer", "description": "Days until the token expires, from 1 to 3650. Omit it for a token that never expires."}}},
		{"type": "object", "required": ["settings"], "properties": {
			"settings": {"type": "array", "items": {"$ref": "#/components/schemas/SettingView"},
				"description": "Every setting, in display order."}}}
	]`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("schemas (-want +got):\n%s", diff)
	}
}

func TestOpenAPIDescribesParametersRequestsAndResponses(t *testing.T) {
	doc := openAPIDocument(t)

	got := []any{
		testsupport.JSONAt(doc, "/paths/~1tokens~1{id}/delete"),
		testsupport.JSONAt(doc, "/paths/~1tokens/post/requestBody"),
	}

	errorResponse := func(description string) string {
		return `{"description": "` + description + `", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/ErrorBody"}}}}`
	}
	want := testsupport.JSON(`[
		{"operationId": "revokeToken", "summary": "Revoke one of your personal API tokens", "tags": ["tokens"],
		 "security": [{"bearerAuth": []}, {"basicAuth": []}],
		 "parameters": [{"name": "id", "in": "path", "required": true, "description": "The token's id.",
			"schema": {"type": "integer"}, "example": 1}],
		 "responses": {
			"204": {"description": "The token is revoked and refused from now on."},
			"401": ` + errorResponse("No valid API token was sent.") + `,
			"404": ` + errorResponse("The caller has no token with this id.") + `,
			"500": ` + errorResponse("The server failed; the body names a request id to give an administrator.") + `}},
		{"required": true, "content": {"application/json": {
			"schema": {"$ref": "#/components/schemas/TokenRequest"},
			"example": {"name": "deploy script", "expires_in_days": 90}}}}
	]`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("operation (-want +got):\n%s", diff)
	}
}

func TestOpenAPIRefusalOfAMissingRoleNamesTheRole(t *testing.T) {
	got := testsupport.JSONAt(openAPIDocument(t), "/paths/~1settings/get/responses/403/description")

	if diff := cmp.Diff("The caller does not hold the admin role.", got); diff != "" {
		t.Errorf("description (-want +got):\n%s", diff)
	}
}

// specCaller arranges an application from f and returns a client to call
// every documented operation with.
type specCaller func(t *testing.T, f *testsupport.Factory) *testsupport.Browser

var specCallers = map[string]specCaller{
	"anonymous": func(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
		return startBrowser(t, f)
	},
	"an administrator's token": func(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
		h, secret := adminWithToken(t, f, `{"name":"ci"}`)
		return tokenClient(h, secret)
	},
	"the token of a user without roles": func(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
		h, secret := adminWithToken(t, f, `{"name":"ci"}`)
		f.StoreStub.RevokeRoles(adminEmail)
		return tokenClient(h, secret)
	},
	"an administrator's browser session": func(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
		return signedInAdmin(t, f)
	},
	"a token while the store fails": func(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
		h, secret := adminWithToken(t, f, `{"name":"ci"}`)
		f.StoreStub.Fail()
		return tokenClient(h, secret)
	},
}

func TestEveryDocumentedOperationAnswersWithADocumentedStatus(t *testing.T) {
	for name, arrange := range specCallers {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			c := arrange(t, f)
			ops := testsupport.OpenAPIOperations(c.Get("/api/v1/openapi.json").Body)

			for _, op := range ops {
				got := c.Send(op.Method, op.Target, op.Body)

				if !listed(op.Statuses, got.Status) {
					t.Errorf("%s %s (%s) answered %d, which the document does not list among %v:\n%s",
						op.Method, op.Target, op.ID, got.Status, op.Statuses, got.Body)
				}
			}
		})
	}
}

func listed(statuses []int, status int) bool {
	for _, s := range statuses {
		if s == status {
			return true
		}
	}
	return false
}
