package demo

import "github.com/dakriy/doorcode/internal/connector"

// Environment is the process environment with the variables the demo sets
// itself, such as the throwaway secret key, in front, and the ones it
// presets, such as the mail server, behind.
type Environment struct {
	Process connector.Environment
	Set     map[string]string
	Preset  map[string]string
}

var _ connector.Environment = Environment{}

// Lookup returns the demo's own value of name when it sets one, else the
// process's, else the demo's preset.
func (e Environment) Lookup(name string) (string, bool) {
	if value, ok := e.Set[name]; ok {
		return value, true
	}
	if value, ok := e.Process.Lookup(name); ok {
		return value, true
	}
	value, ok := e.Preset[name]
	return value, ok
}

// Mailpit presets the mail settings to a Mailpit on this machine at its
// default SMTP port, which takes any message in the clear and delivers
// none.
var Mailpit = map[string]string{
	"DOORCODE_MAIL_HOST":     "localhost",
	"DOORCODE_MAIL_PORT":     "1025",
	"DOORCODE_MAIL_SECURITY": "none",
	"DOORCODE_MAIL_FROM":     "doorcode demo <doorcode@example.com>",
}
