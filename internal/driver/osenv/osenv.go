// Package osenv implements connector.Environment over the process
// environment.
package osenv

import "os"

// Environment reads the process environment.
type Environment struct{}

// Lookup returns the variable's value and whether it is set.
func (Environment) Lookup(name string) (string, bool) {
	return os.LookupEnv(name)
}
