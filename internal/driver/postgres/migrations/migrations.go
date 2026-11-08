// Package migrations holds the schema migrations as embedded SQL files,
// applied in file-name order by the postgres store.
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
