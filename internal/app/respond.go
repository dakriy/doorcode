package app

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/dakriy/doorcode/internal/check"
)

// requestIDBytes is how much entropy names one failed request.
const requestIDBytes = 8

const jsonType = "application/json"

// maxJSONBody caps the size of a JSON request body.
const maxJSONBody = 65536 // 64 KiB

// decodeJSON reads r's JSON body into v.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(v)
}

type errorBody struct {
	Error     string `json:"error" doc:"What went wrong."`
	RequestID string `json:"request_id,omitempty" doc:"Names the failure in the server log; present only when the server failed."`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", jsonType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// internalError logs err under a fresh request id and tells the client only
// the id, so an operator can find the cause in the log.
func (a *App) internalError(w http.ResponseWriter, r *http.Request, err error) {
	id := a.logFailure(r, err)
	writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal error", RequestID: id})
}

// refusalStatus pairs a refusal a service reports with the status the API
// answers it with.
type refusalStatus struct {
	err    error
	status int
}

// refuse answers an API request with what err means: field problems are a
// bad request, a refusal among known is answered with its status, and
// anything else is a failure.
func (a *App) refuse(w http.ResponseWriter, r *http.Request, err error, known []refusalStatus) {
	var problems check.Problems
	if errors.As(err, &problems) {
		writeError(w, http.StatusBadRequest, problems.Error())
		return
	}
	for _, k := range known {
		if errors.Is(err, k.err) {
			writeError(w, k.status, err.Error())
			return
		}
	}
	a.internalError(w, r, err)
}
