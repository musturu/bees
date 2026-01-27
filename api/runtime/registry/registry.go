package registry

import (
	"fmt"
	"strings"
	"sync"
)

// ServiceRegistry is an interface that can resolve a service by name and config.
type ServiceRegistry interface {
	Resolve(name string, configData []byte) (any, error)
}

// RuntimeEntry stores metadata for a registered runtime.
type RuntimeEntry struct {
	Schema     string      // Embedded CUE schema as a string
	ConfigType interface{} // Pointer to runtime struct (e.g., &http.Runtime{})
}

// ServiceEntry stores metadata for a registered service.
type ServiceEntry struct {
	SchemaPath string
	ConfigType interface{}
}

var (
	mu                sync.RWMutex
	runtimes          = make(map[string]RuntimeEntry)
	serviceRegistries = make(map[string]ServiceRegistry)
)

// RegisterRuntime registers a runtime with an embedded CUE schema.
// - name: runtime name (case-insensitive)
// - schema: embedded CUE schema as a string
// - runtimeType: pointer to runtime struct (e.g., &http.Runtime{})
func RegisterRuntime(name string, schema string, runtimeType interface{}) error {
	if schema == "" {
		return fmt.Errorf("schema cannot be empty")
	}
	if runtimeType == nil {
		return fmt.Errorf("runtimeType cannot be nil")
	}

	mu.Lock()
	defer mu.Unlock()
	runtimes[strings.ToLower(name)] = RuntimeEntry{
		Schema:     schema,
		ConfigType: runtimeType,
	}
	return nil
}

// GetRuntime returns the RuntimeEntry for a runtime name.
func GetRuntime(name string) (RuntimeEntry, bool) {
	mu.RLock()
	defer mu.RUnlock()
	entry, ok := runtimes[strings.ToLower(name)]
	return entry, ok
}

// RegisterServiceRegistry registers a service registry for a runtime.
func RegisterServiceRegistry(runtimeName string, reg ServiceRegistry) error {
	if reg == nil {
		return fmt.Errorf("service registry cannot be nil")
	}

	mu.Lock()
	defer mu.Unlock()
	serviceRegistries[strings.ToLower(runtimeName)] = reg
	return nil
}

// GetServiceRegistry returns the service registry for a runtime.
func GetServiceRegistry(runtimeName string) (ServiceRegistry, bool) {
	mu.RLock()
	defer mu.RUnlock()
	reg, ok := serviceRegistries[strings.ToLower(runtimeName)]
	return reg, ok
}

// All returns all registered runtime names.
func All() []string {
	mu.RLock()
	defer mu.RUnlock()
	var names []string
	for name := range runtimes {
		names = append(names, name)
	}
	return names
}
