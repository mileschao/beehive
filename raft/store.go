package raft

// Persistent wraps the two save and recover method.
type Persistent interface {
	// Save saves the store into bytes.
	Save() ([]byte, error)
	// Recover recovers the store from bytes.
	Recover(b []byte) error
}

// Store represents an application defined state. Changes in this store are
// only applied through Apply.
type Store interface {
	Persistent
	// Apply applies a request and returns the response.
	Apply(req interface{}) interface{}
}