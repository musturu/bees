package registry

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"cuelang.org/go/cue"
)

// RegistryEntry represents a configurable element in the framework.
// It defines how to load, validate, and unmarshal configuration for a specific type.
type RegistryEntry struct {
	// AllowedPath is the path in the config where this element can be found.
	// Examples: "runtimes", "runtimes.http", "services", "handlers"
	// Empty string means root level.
	AllowedPath string

	// Kind is a type discriminator that identifies this configuration type.
	// Examples: "http", "grpc", "userService", "authHandler"
	Kind string

	// Schema is the loaded CUE definition for validation.
	// Can be nil if no validation is needed.
	Schema *cue.Value

	// Struct is the target Go struct to unmarshal into.
	// Can be a pointer to a struct or reflect.Type for reflection-based instantiation.
	Struct interface{}
}

// Registry is the central registry for all configurable framework elements.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*RegistryEntry // Keyed by kind
	pathMap map[string][]string       // Maps AllowedPath to slice of kinds
}

var globalRegistry = &Registry{
	entries: make(map[string]*RegistryEntry),
	pathMap: make(map[string][]string),
}

// Register adds a new entry to the global registry.
// Returns an error if the entry is invalid or if the kind is already registered.
func Register(entry *RegistryEntry) error {
	if entry == nil {
		return fmt.Errorf("entry cannot be nil")
	}

	if entry.Kind == "" {
		return fmt.Errorf("entry kind cannot be empty")
	}

	if entry.Struct == nil {
		return fmt.Errorf("entry struct cannot be nil for kind %s", entry.Kind)
	}

	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	// Check for duplicate kind registration
	if _, exists := globalRegistry.entries[entry.Kind]; exists {
		return fmt.Errorf("entry with kind %q already registered", entry.Kind)
	}

	// Store the entry
	globalRegistry.entries[entry.Kind] = entry

	// Add to path map for efficient lookup by path
	path := entry.AllowedPath
	if path == "" {
		path = "__root__"
	}
	globalRegistry.pathMap[path] = append(globalRegistry.pathMap[path], entry.Kind)

	return nil
}

// Get retrieves an entry by its kind.
func Get(kind string) (*RegistryEntry, bool) {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	entry, ok := globalRegistry.entries[kind]
	return entry, ok
}

// GetByPath retrieves all entries registered for a given path.
func GetByPath(path string) []*RegistryEntry {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	if path == "" {
		path = "__root__"
	}

	kinds := globalRegistry.pathMap[path]
	entries := make([]*RegistryEntry, len(kinds))
	for i, kind := range kinds {
		entries[i] = globalRegistry.entries[kind]
	}

	return entries
}

// All returns all registered entries.
func All() []*RegistryEntry {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	entries := make([]*RegistryEntry, 0, len(globalRegistry.entries))
	for _, entry := range globalRegistry.entries {
		entries = append(entries, entry)
	}
	return entries
}

// AllByPath returns all registered entries, grouped by AllowedPath.
func AllByPath() map[string][]*RegistryEntry {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	result := make(map[string][]*RegistryEntry)
	for path, kinds := range globalRegistry.pathMap {
		displayPath := path
		if path == "__root__" {
			displayPath = ""
		}
		for _, kind := range kinds {
			result[displayPath] = append(result[displayPath], globalRegistry.entries[kind])
		}
	}
	return result
}

// Clear resets the global registry. Useful for testing.
func Clear() {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	globalRegistry.entries = make(map[string]*RegistryEntry)
	globalRegistry.pathMap = make(map[string][]string)
}

// ResolveStruct creates a new instance of the target struct from the entry.
// If Struct is already a pointer, it returns a new pointer of that type.
// If Struct is a reflect.Type, it instantiates it.
func (e *RegistryEntry) ResolveStruct() (interface{}, error) {
	if rt, ok := e.Struct.(reflect.Type); ok {
		// Create new instance from reflect.Type
		return reflect.New(rt).Interface(), nil
	}

	// Assume it's already a pointer to a struct
	// Get the type and create a new zero value
	t := reflect.TypeOf(e.Struct)
	if t.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("struct must be a pointer for kind %q", e.Kind)
	}

	// Create a new pointer to the same type
	elemType := t.Elem()
	newValue := reflect.New(elemType)
	return newValue.Interface(), nil
}

// PathSegments splits an allowed path into segments.
// Examples:
//
//	"runtimes" -> ["runtimes"]
//	"runtimes.http" -> ["runtimes", "http"]
//	"" -> []
func PathSegments(path string) []string {
	if path == "" {
		return []string{}
	}
	return strings.Split(path, ".")
}

// NormalizeKind normalizes a kind string to lowercase for case-insensitive comparison.
func NormalizeKind(kind string) string {
	return strings.ToLower(kind)
}
