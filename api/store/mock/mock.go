package mockstore

// MockStore is a lightweight in-memory stub for tests and local development.
// It satisfies Storer but does not maintain external state.
type MockStore struct {
	connected bool
}

// Connect marks the mock store as connected.
func (m *MockStore) Connect() error {
	m.connected = true
	return nil
}

// Close resets the connection flag.
func (m *MockStore) Close() error {
	m.connected = false
	return nil
}
