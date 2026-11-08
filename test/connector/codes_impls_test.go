package connector_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/codesfile"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// codesImpl constructs one implementation of connector.Codes in a given state.
// Every Codes test runs against each entry, so the stub and the real file
// connector are held to the same behavior.
type codesImpl struct {
	name    string
	holding func(t *testing.T, content []byte) connector.Codes
	missing func(t *testing.T) connector.Codes
}

var codesImpls = []codesImpl{
	{
		name: "stub",
		holding: func(_ *testing.T, content []byte) connector.Codes {
			return testsupport.Codes{Content: content}
		},
		missing: func(_ *testing.T) connector.Codes {
			return testsupport.Codes{Err: testsupport.ErrUnavailable}
		},
	},
	{
		name: "file",
		holding: func(t *testing.T, content []byte) connector.Codes {
			path := filepath.Join(t.TempDir(), "codes")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			return codesfile.File{Path: path}
		},
		missing: func(t *testing.T) connector.Codes {
			return codesfile.File{Path: filepath.Join(t.TempDir(), "absent")}
		},
	},
}
