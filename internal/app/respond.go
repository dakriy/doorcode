package app

import (
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/dakriy/doorcode/internal/connector"
)

// requestIDBytes is how much entropy names one failed request.
const requestIDBytes = 8

type errorBody struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// internalError logs err under a fresh request id and tells the client only
// the id, so an operator can find the cause in the log.
func (a *App) internalError(w http.ResponseWriter, err error) {
	id := hex.EncodeToString(a.entropy.Bytes(requestIDBytes))
	a.log.Error("request failed",
		connector.Field{Key: "request_id", Value: id},
		connector.Field{Key: "error", Value: err.Error()})
	writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal error", RequestID: id})
}
