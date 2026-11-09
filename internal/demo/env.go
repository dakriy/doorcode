package demo

import "github.com/dakriy/doorcode/internal/connector"

// Environment is the process environment with the variables the demo sets
// itself, such as the throwaway secret key, in front.
type Environment struct {
	Process connector.Environment
	Set     map[string]string
}

var _ connector.Environment = Environment{}

// Lookup returns the demo's own value of name when it sets one, and
// otherwise the process's.
func (e Environment) Lookup(name string) (string, bool) {
	if value, ok := e.Set[name]; ok {
		return value, true
	}
	return e.Process.Lookup(name)
}
