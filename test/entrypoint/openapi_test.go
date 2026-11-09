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
	t.Parallel()
	got := testsupport.Get(start(t, testsupport.NewFactory()).Handler(), "/api/v1/openapi.json")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: string(app.OpenAPI())}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestOpenAPIListsEveryOperationWithTheStatusesItAnswers(t *testing.T) {
	t.Parallel()
	got := testsupport.OpenAPIOperations(openAPIDocument(t))

	groupBody := `{"slug":"night-staff","name":"Night staff","description":"Security and cleaning crews.",` +
		`"roles":["view-logs"],"preassigned_user_ids":["100234","100877"]}`
	scheduleBody := `{"name":"Night shift","starts_on":"2026-01-01","ends_on":"2026-12-31",` +
		`"windows":[{"days":["mon","tue","wed","thu","fri"],"start":"22:00","end":"06:00"}],"excluded_dates":["2026-12-25"]}`
	userBody := `{"email":"ann.smith@example.com","user_id":"100234","first_name":"Ann","last_name":"Smith",` +
		`"expires_at":"2030-06-30T00:00:00Z"}`
	doorBody := `{"name":"North entrance","location":"Building A, ground floor","notes":"Strike replaced in 2026.",` +
		`"public":true,"open_schedule_ids":[1],"zone_ids":[1]}`
	zoneBody := `{"name":"Building A","description":"Every outside door of building A.","door_ids":[1]}`
	ruleBody := `{"group_id":1,"schedule_id":1,"door_ids":[1],"zone_ids":[1]}`
	requestBody := `{"title":"Lab access","body":"I start in the electronics lab on Monday and need to get in from 8:00."}`
	overrideBody := `{"kind":"locked","start":"2026-12-24T18:00:00Z","end":"2026-12-27T06:00:00Z","reason":"Closed over the holiday."}`
	want := []testsupport.APIOperation{
		{Method: "GET", Target: "/api/v1/access-rules", ID: "listAccessRules", Statuses: []int{200, 400, 401, 403, 500}},
		{Method: "POST", Target: "/api/v1/access-rules", ID: "createAccessRule", Body: ruleBody, Statuses: []int{201, 400, 401, 403, 500}},
		{Method: "DELETE", Target: "/api/v1/access-rules/1", ID: "deleteAccessRule", Statuses: []int{204, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/access-rules/1", ID: "getAccessRule", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "PUT", Target: "/api/v1/access-rules/1", ID: "updateAccessRule", Body: ruleBody, Statuses: []int{200, 400, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/doors", ID: "listDoors", Statuses: []int{200, 400, 401, 403, 500}},
		{Method: "POST", Target: "/api/v1/doors", ID: "createDoor", Body: doorBody, Statuses: []int{201, 400, 401, 403, 500}},
		{Method: "DELETE", Target: "/api/v1/doors/1", ID: "deleteDoor", Statuses: []int{204, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/doors/1", ID: "getDoor", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "PUT", Target: "/api/v1/doors/1", ID: "updateDoor", Body: doorBody, Statuses: []int{200, 400, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/config", ID: "getDoorConfig", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "PUT", Target: "/api/v1/doors/1/config", ID: "updateDoorConfig", Body: `{"overrides":{"log_level":"debug","strike_pulse_ms":"5000"}}`,
			Statuses: []int{200, 400, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/credentials", ID: "listDoorCredentials", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "POST", Target: "/api/v1/doors/1/credentials/revoke", ID: "revokeDoorCredentials", Statuses: []int{204, 401, 403, 404, 500}},
		{Method: "POST", Target: "/api/v1/doors/1/credentials/rotate", ID: "rotateDoorCredentials", Statuses: []int{204, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/entrants", ID: "listDoorEntrants", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/firmware", ID: "getDoorFirmware", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "PUT", Target: "/api/v1/doors/1/firmware", ID: "setDoorFirmware", Body: `{"firmware_id":1}`, Statuses: []int{200, 400, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/firmware/1.4.3", ID: "downloadFirmware", Statuses: []int{200, 403, 404, 500}},
		{Method: "POST", Target: "/api/v1/doors/1/open", ID: "openDoor", Statuses: []int{200, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/overrides", ID: "listDoorOverrides", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "POST", Target: "/api/v1/doors/1/overrides", ID: "createDoorOverride", Body: overrideBody, Statuses: []int{201, 400, 401, 403, 404, 500}},
		{Method: "DELETE", Target: "/api/v1/doors/1/overrides/1", ID: "deleteDoorOverride", Statuses: []int{204, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/overrides/1", ID: "getDoorOverride", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "PUT", Target: "/api/v1/doors/1/overrides/1", ID: "updateDoorOverride", Body: overrideBody, Statuses: []int{200, 400, 401, 403, 404, 500}},
		{Method: "POST", Target: "/api/v1/doors/1/overrides/1/cancel", ID: "cancelDoorOverride", Statuses: []int{200, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/pictures", ID: "listDoorPictures", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "POST", Target: "/api/v1/doors/1/pictures", ID: "uploadDoorPicture", Statuses: []int{201, 400, 401, 403, 404, 413, 500}},
		{Method: "DELETE", Target: "/api/v1/doors/1/pictures/1", ID: "deleteDoorPicture", Statuses: []int{204, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/pictures/1", ID: "getDoorPicture", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "POST", Target: "/api/v1/doors/1/pictures/1/primary", ID: "setPrimaryDoorPicture", Statuses: []int{204, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/pictures/1/thumbnail", ID: "getDoorPictureThumbnail", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "POST", Target: "/api/v1/doors/1/restore", ID: "restoreDoor", Statuses: []int{204, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/state", ID: "getDoorState", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/telemetry", ID: "listDoorTelemetry", Statuses: []int{200, 400, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/telemetry/hourly", ID: "listDoorTelemetryHourly", Statuses: []int{200, 400, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/doors/1/timeline", ID: "getDoorTimeline", Statuses: []int{200, 400, 401, 403, 404, 500}},
		{Method: "POST", Target: "/api/v1/doors/1/update", ID: "updateDoor", Statuses: []int{200, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/enrollments", ID: "listEnrollments", Statuses: []int{200, 401, 403, 500}},
		{Method: "POST", Target: "/api/v1/enrollments/a4cf12f00e3b/approve", ID: "approveEnrollment", Body: `{"door_id":1}`,
			Statuses: []int{200, 400, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/events", ID: "listEvents", Statuses: []int{200, 400, 401, 403, 500}},
		{Method: "GET", Target: "/api/v1/firmware", ID: "listFirmware", Statuses: []int{200, 401, 403, 500}},
		{Method: "POST", Target: "/api/v1/firmware", ID: "uploadFirmware", Statuses: []int{201, 400, 401, 403, 413, 500}},
		{Method: "PUT", Target: "/api/v1/firmware/default", ID: "setDefaultFirmware", Body: `{"firmware_id":1}`, Statuses: []int{204, 400, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/firmware/rollout", ID: "getRollout", Statuses: []int{200, 401, 403, 500}},
		{Method: "GET", Target: "/api/v1/firmware/settings", ID: "listRolloutSettings", Statuses: []int{200, 401, 403, 500}},
		{Method: "PUT", Target: "/api/v1/firmware/settings", ID: "updateRolloutSettings",
			Body: `{"update_concurrency":"5","update_window_days":"sat,sun","update_window_start":"02:00"}`, Statuses: []int{200, 400, 401, 403, 500}},
		{Method: "DELETE", Target: "/api/v1/firmware/1", ID: "deleteFirmware", Statuses: []int{204, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/firmware/1", ID: "getFirmware", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/groups", ID: "listGroups", Statuses: []int{200, 401, 403, 500}},
		{Method: "POST", Target: "/api/v1/groups", ID: "createGroup", Body: groupBody, Statuses: []int{201, 400, 401, 403, 500}},
		{Method: "DELETE", Target: "/api/v1/groups/1", ID: "deleteGroup", Statuses: []int{204, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/groups/1", ID: "getGroup", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "PUT", Target: "/api/v1/groups/1", ID: "updateGroup", Body: groupBody, Statuses: []int{200, 400, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/groups/1/members", ID: "listGroupMembers", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "DELETE", Target: "/api/v1/groups/1/members/1", ID: "removeGroupMember", Statuses: []int{204, 401, 403, 404, 409, 500}},
		{Method: "PUT", Target: "/api/v1/groups/1/members/1", ID: "addGroupMember", Statuses: []int{204, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/me/access", ID: "getMyAccess", Statuses: []int{200, 400, 401, 500}},
		{Method: "GET", Target: "/api/v1/me/code", ID: "getMyCode", Statuses: []int{200, 401, 500}},
		{Method: "PUT", Target: "/api/v1/me/code", ID: "setMyCode", Body: `{"digits":"4821"}`, Statuses: []int{204, 400, 401, 409, 500}},
		{Method: "GET", Target: "/api/v1/me/entries", ID: "listMyEntries", Statuses: []int{200, 400, 401, 500}},
		{Method: "PUT", Target: "/api/v1/me/password", ID: "setMyPassword", Body: `{"current_password":"old horse battery staple","new_password":"correct horse battery staple"}`,
			Statuses: []int{204, 400, 401, 403, 500}},
		{Method: "GET", Target: "/api/v1/me/requests", ID: "listMyRequests", Statuses: []int{200, 401, 500}},
		{Method: "POST", Target: "/api/v1/me/requests", ID: "createMyRequest", Body: requestBody, Statuses: []int{201, 400, 401, 500}},
		{Method: "DELETE", Target: "/api/v1/me/requests/1", ID: "deleteMyRequest", Statuses: []int{204, 401, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/me/requests/1", ID: "getMyRequest", Statuses: []int{200, 401, 404, 500}},
		{Method: "PUT", Target: "/api/v1/me/requests/1", ID: "updateMyRequest", Body: requestBody, Statuses: []int{200, 400, 401, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/openapi.json", ID: "getOpenAPI", Statuses: []int{200}},
		{Method: "GET", Target: "/api/v1/requests", ID: "listRequests", Statuses: []int{200, 400, 401, 403, 500}},
		{Method: "GET", Target: "/api/v1/requests/1", ID: "getRequest", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "POST", Target: "/api/v1/requests/1/reopen", ID: "reopenRequest", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "PUT", Target: "/api/v1/requests/1/reply", ID: "replyToRequest", Body: `{"reply":"You can enter the lab on weekdays from Monday."}`,
			Statuses: []int{200, 400, 401, 403, 404, 500}},
		{Method: "POST", Target: "/api/v1/requests/1/resolve", ID: "resolveRequest", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/schedules", ID: "listSchedules", Statuses: []int{200, 401, 403, 500}},
		{Method: "POST", Target: "/api/v1/schedules", ID: "createSchedule", Body: scheduleBody, Statuses: []int{201, 400, 401, 403, 500}},
		{Method: "DELETE", Target: "/api/v1/schedules/1", ID: "deleteSchedule", Statuses: []int{204, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/schedules/1", ID: "getSchedule", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "PUT", Target: "/api/v1/schedules/1", ID: "updateSchedule", Body: scheduleBody, Statuses: []int{200, 400, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/schedules/1/occurrences", ID: "listScheduleOccurrences", Statuses: []int{200, 400, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/settings", ID: "listSettings", Statuses: []int{200, 401, 403, 500}},
		{Method: "GET", Target: "/api/v1/settings/site_name", ID: "getSetting", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "PUT", Target: "/api/v1/settings/site_name", ID: "updateSetting", Body: `{"value":"North Campus"}`,
			Statuses: []int{200, 400, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/stats/doors", ID: "getDoorStats", Statuses: []int{200, 400, 401, 403, 500}},
		{Method: "GET", Target: "/api/v1/stats/entries", ID: "getEntryStats", Statuses: []int{200, 400, 401, 403, 500}},
		{Method: "GET", Target: "/api/v1/stats/firmware", ID: "getFirmwareStats", Statuses: []int{200, 401, 403, 500}},
		{Method: "GET", Target: "/api/v1/stats/now", ID: "getLiveCounts", Statuses: []int{200, 401, 403, 500}},
		{Method: "GET", Target: "/api/v1/tokens", ID: "listTokens", Statuses: []int{200, 401, 500}},
		{Method: "POST", Target: "/api/v1/tokens", ID: "createToken", Body: `{"name":"deploy script","expires_in_days":90}`,
			Statuses: []int{201, 400, 401, 500}},
		{Method: "DELETE", Target: "/api/v1/tokens/1", ID: "revokeToken", Statuses: []int{204, 401, 404, 500}},
		{Method: "GET", Target: "/api/v1/users", ID: "listUsers", Statuses: []int{200, 400, 401, 403, 500}},
		{Method: "POST", Target: "/api/v1/users", ID: "createUser", Body: userBody, Statuses: []int{201, 400, 401, 403, 500}},
		{Method: "POST", Target: "/api/v1/users/import", ID: "importUsers", Statuses: []int{200, 400, 401, 403, 413, 500}},
		{Method: "POST", Target: "/api/v1/users/import/preview", ID: "previewUserImport", Statuses: []int{200, 400, 401, 403, 413, 500}},
		{Method: "DELETE", Target: "/api/v1/users/1", ID: "deleteUser", Statuses: []int{204, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/users/1", ID: "getUser", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "PUT", Target: "/api/v1/users/1", ID: "updateUser", Body: userBody, Statuses: []int{200, 400, 401, 403, 404, 409, 500}},
		{Method: "GET", Target: "/api/v1/users/1/access", ID: "getUserAccess", Statuses: []int{200, 400, 401, 403, 404, 500}},
		{Method: "DELETE", Target: "/api/v1/users/1/code", ID: "clearUserCode", Statuses: []int{204, 401, 403, 404, 500}},
		{Method: "PUT", Target: "/api/v1/users/1/code", ID: "setUserCode", Body: `{"code":"73914862"}`,
			Statuses: []int{204, 400, 401, 403, 404, 500}},
		{Method: "POST", Target: "/api/v1/users/1/restore", ID: "restoreUser", Statuses: []int{204, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/users/1/tokens", ID: "listUserTokens", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "POST", Target: "/api/v1/users/1/tokens", ID: "createUserToken", Body: `{"name":"door sync","expires_in_days":30}`,
			Statuses: []int{201, 400, 401, 403, 404, 500}},
		{Method: "DELETE", Target: "/api/v1/users/1/tokens/1", ID: "revokeUserToken", Statuses: []int{204, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/zones", ID: "listZones", Statuses: []int{200, 401, 403, 500}},
		{Method: "POST", Target: "/api/v1/zones", ID: "createZone", Body: zoneBody, Statuses: []int{201, 400, 401, 403, 500}},
		{Method: "DELETE", Target: "/api/v1/zones/1", ID: "deleteZone", Statuses: []int{204, 401, 403, 404, 500}},
		{Method: "GET", Target: "/api/v1/zones/1", ID: "getZone", Statuses: []int{200, 401, 403, 404, 500}},
		{Method: "PUT", Target: "/api/v1/zones/1", ID: "updateZone", Body: zoneBody, Statuses: []int{200, 400, 401, 403, 404, 500}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("operations (-want +got):\n%s", diff)
	}
}

func TestOpenAPIDescribesTheAPIAndHowToAuthenticate(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	doc := openAPIDocument(t)
	schemas := []string{"TokenView", "CreatedToken", "TokenRequest", "SettingList", "DoorConfigRequest"}

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
				"description": "Every setting, in display order."}}},
		{"type": "object", "required": ["overrides"], "properties": {
			"overrides": {"type": "object", "additionalProperties": {"type": "string"},
				"description": "The door's own values, each keyed by the name of a field of the effective configuration; the site's defaults apply to the fields left out. They replace the overrides the door had."}}}
	]`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("schemas (-want +got):\n%s", diff)
	}
}

func TestOpenAPIDescribesFractionsAsNumbers(t *testing.T) {
	t.Parallel()

	got := testsupport.JSONAt(openAPIDocument(t), "/components/schemas/SpreadView")

	want := testsupport.JSON(`{"type": "object", "required": ["mean", "min", "max"], "properties": {
		"mean": {"type": "number", "description": "The mean of the values."},
		"min": {"type": "integer", "description": "The smallest value."},
		"max": {"type": "integer", "description": "The largest value."}}}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("schema (-want +got):\n%s", diff)
	}
}

func TestOpenAPIDescribesParametersRequestsAndResponses(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestOpenAPIDescribesPictureUploadsAndImages(t *testing.T) {
	t.Parallel()
	doc := openAPIDocument(t)

	got := []any{
		testsupport.JSONAt(doc, "/paths/~1doors~1{id}~1pictures/post/requestBody"),
		testsupport.JSONAt(doc, "/components/schemas/PictureUpload"),
		testsupport.JSONAt(doc, "/paths/~1doors~1{id}~1pictures~1{picture}~1thumbnail/get/responses/200"),
	}

	want := testsupport.JSON(`[
		{"required": true, "content": {"multipart/form-data": {"schema": {"$ref": "#/components/schemas/PictureUpload"}}}},
		{"type": "object", "required": ["picture"], "properties": {
			"picture": {"type": "string", "format": "binary", "description": "The image: JPEG, PNG, GIF, or WebP."}}},
		{"description": "The image reduced to at most 320 pixels on its longest side. Anyone signed in may read the pictures of a door the portal shows; other doors' need the manage-doors role.",
		 "content": {"image/jpeg": {"schema": {"type": "string", "format": "binary"}}}}
	]`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("document (-want +got):\n%s", diff)
	}
}
