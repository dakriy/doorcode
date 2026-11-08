// Package slogjson implements connector.Log as JSON lines through log/slog.
package slogjson

import (
	"io"
	"log/slog"

	"github.com/dakriy/doorcode/internal/connector"
)

// Log writes one JSON object per line.
type Log struct {
	logger *slog.Logger
}

// New returns a Log writing to w.
func New(w io.Writer) Log {
	return Log{logger: slog.New(slog.NewJSONHandler(w, nil))}
}

// Info writes an info line.
func (l Log) Info(msg string, fields ...connector.Field) {
	l.logger.Info(msg, attrs(fields)...)
}

// Error writes an error line.
func (l Log) Error(msg string, fields ...connector.Field) {
	l.logger.Error(msg, attrs(fields)...)
}

func attrs(fields []connector.Field) []any {
	out := make([]any, 0, len(fields))
	for _, f := range fields {
		out = append(out, slog.String(f.Key, f.Value))
	}
	return out
}
