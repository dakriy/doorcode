// Package testsupport holds the hand-written connector stubs, the stub
// factory, and the helpers the entrypoint suite drives the handler with.
package testsupport

import (
	"errors"

	"github.com/dakriy/doorcode/internal/connector"
)

// ErrUnavailable is what an unavailable stub connector reports.
var ErrUnavailable = errors.New("stub: unavailable")

// Codes is the stub connector.Codes: it returns Content and Err as given.
type Codes struct {
	Content []byte
	Err     error
}

// Load returns the stub's configured result.
func (c Codes) Load() ([]byte, error) {
	return c.Content, c.Err
}

// Factory is the stub connector.Factory.
type Factory struct {
	CodesStub Codes
}

// Codes returns the stub codes connector.
func (f Factory) Codes() connector.Codes {
	return f.CodesStub
}

// WithCodes returns a stub factory whose code list is the given lines.
func WithCodes(content string) Factory {
	return Factory{CodesStub: Codes{Content: []byte(content)}}
}

// WithUnavailableCodes returns a stub factory whose code list cannot be read.
func WithUnavailableCodes() Factory {
	return Factory{CodesStub: Codes{Err: ErrUnavailable}}
}
