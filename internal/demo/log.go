package demo

import (
	"sync"

	"github.com/dakriy/doorcode/internal/connector"
)

// Log writes every line to the log it wraps and remembers the lines that
// carry a password, so the demo can read the one the application made up
// for the first administrator.
type Log struct {
	connector.Log
	mu        sync.Mutex
	passwords [][]connector.Field
}

// NewLog wraps log.
func NewLog(log connector.Log) *Log {
	return &Log{Log: log}
}

var _ connector.Log = (*Log)(nil)

// Info writes an informational line.
func (l *Log) Info(msg string, fields ...connector.Field) {
	l.remember(fields)
	l.Log.Info(msg, fields...)
}

// Error writes an error line.
func (l *Log) Error(msg string, fields ...connector.Field) {
	l.remember(fields)
	l.Log.Error(msg, fields...)
}

func (l *Log) remember(fields []connector.Field) {
	if _, ok := fieldOf(fields, "password"); !ok {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.passwords = append(l.passwords, append([]connector.Field(nil), fields...))
}

// Password is the password on the first line logged with a password and
// a field named key, that field's value, and whether there is such a line.
func (l *Log) Password(key string) (password, value string, found bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.passwords {
		if value, ok := fieldOf(line, key); ok {
			password, _ = fieldOf(line, "password")
			return password, value, true
		}
	}
	return "", "", false
}

func fieldOf(line []connector.Field, key string) (string, bool) {
	for _, f := range line {
		if f.Key == key {
			return f.Value, true
		}
	}
	return "", false
}
