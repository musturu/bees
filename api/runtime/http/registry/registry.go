package registry

import (
	httprt "bees/api/runtime/http"
	basreg "bees/api/runtime/registry"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// httpRuntimeSchema is the embedded CUE schema for HTTP runtime configuration.
const httpRuntimeSchema = `
{
	addr?: string | *":http"
	read_timeout?: string | *"10s"
	write_timeout?: string | *"30s"
	idle_timeout?: string | *"120s"
	tls_cert_file?: string
	tls_key_file?: string
	services?: {[string]: any}
}
`

var (
	mu       sync.RWMutex
	services = make(map[string]basreg.ServiceEntry)
)

func init() {
	// Register the HTTP runtime with embedded CUE schema
	// ConfigType is the Runtime struct itself (with embedded Config)
	if err := basreg.RegisterRuntime("http", httpRuntimeSchema, &httprt.Runtime{}); err != nil {
		fmt.Printf("failed to register http runtime: %v\n", err)
	}

	// Register the HTTP service registry
	if err := basreg.RegisterServiceRegistry("http", &httpServiceRegistry{}); err != nil {
		fmt.Printf("failed to register http service registry: %v\n", err)
	}
}

type httpServiceRegistry struct{}

// Resolve implements registry.ServiceRegistry.
// It unmarshals service config data into the registered config struct.
// CUE validation is assumed to have been done by the caller.
func (h *httpServiceRegistry) Resolve(name string, configData []byte) (any, error) {
	entry, ok := GetService(name)
	if !ok {
		mu.RLock()
		var names []string
		for k := range services {
			names = append(names, k)
		}
		mu.RUnlock()
		return nil, fmt.Errorf("http: unknown service %q, registered: %v", name, names)
	}

	// Unmarshal JSON config into the config struct
	if err := json.Unmarshal(configData, entry.ConfigType); err != nil {
		return nil, fmt.Errorf("http: failed to unmarshal config for service %q: %w", name, err)
	}

	return entry.ConfigType, nil
}

// RegisterService registers an HTTP service by name with its schema file path and config type.
func RegisterService(name string, schemaPath string, configType interface{}) {
	mu.Lock()
	defer mu.Unlock()
	ln := strings.ToLower(name)
	services[ln] = basreg.ServiceEntry{
		SchemaPath: schemaPath,
		ConfigType: configType,
	}
	fmt.Printf("http registry: registered service %q with schema %q\n", ln, schemaPath)
}

// GetService returns the ServiceEntry for an HTTP service name.
func GetService(name string) (basreg.ServiceEntry, bool) {
	mu.RLock()
	defer mu.RUnlock()
	entry, ok := services[strings.ToLower(name)]
	return entry, ok
}
