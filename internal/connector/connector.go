// Package connector declares every interface through which application code
// reaches the outside world, and the factory that supplies them.
package connector

// Codes supplies the raw contents of the door-code list.
type Codes interface {
	Load() ([]byte, error)
}

// Factory constructs connectors. It has one method per connector.
type Factory interface {
	Codes() Codes
}
