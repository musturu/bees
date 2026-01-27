package registry

import (
	grpcrt "bees/api/runtime/grpc"
	basreg "bees/api/runtime/registry"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// grpcRuntimeSchema is the embedded CUE schema for gRPC runtime configuration.
const grpcRuntimeSchema = `
{
	addr?: string | *":grpc"
	services?: {[string]: any}
}
`

var (
	mu       sync.RWMutex
	services = make(map[string]basreg.ServiceEntry)
)

type grpcServiceRegistry struct{}

func init() {
	// Register the gRPC runtime with embedded CUE schema
	// ConfigType is the Runtime struct itself (with embedded Config)
	if err := basreg.RegisterRuntime("grpc", grpcRuntimeSchema, &grpcrt.Runtime{}); err != nil {
		fmt.Printf("failed to register grpc runtime: %v\n", err)
	}

	// Register the gRPC service registry
	if err := basreg.RegisterServiceRegistry("grpc", &grpcServiceRegistry{}); err != nil {
		fmt.Printf("failed to register grpc service registry: %v\n", err)
	}
}

// Resolve implements registry.ServiceRegistry.
// It unmarshals service config data into the registered config struct.
// CUE validation is assumed to have been done by the caller.
func (g *grpcServiceRegistry) Resolve(name string, configData []byte) (any, error) {
	entry, ok := GetService(name)
	if !ok {
		mu.RLock()
		var names []string
		for k := range services {
			names = append(names, k)
		}
		mu.RUnlock()
		return nil, fmt.Errorf("grpc: unknown service %q, registered: %v", name, names)
	}

	// Unmarshal JSON config into the config struct
	if err := json.Unmarshal(configData, entry.ConfigType); err != nil {
		return nil, fmt.Errorf("grpc: failed to unmarshal config for service %q: %w", name, err)
	}

	return entry.ConfigType, nil
}

// RegisterService registers a gRPC service by name with its schema file path and config type.
func RegisterService(name string, schemaPath string, configType interface{}) {
	mu.Lock()
	defer mu.Unlock()
	services[strings.ToLower(name)] = basreg.ServiceEntry{
		SchemaPath: schemaPath,
		ConfigType: configType,
	}
	fmt.Printf("grpc registry: registered service %q with schema %q\n", strings.ToLower(name), schemaPath)
}

// GetService returns the ServiceEntry for a gRPC service name.
func GetService(name string) (basreg.ServiceEntry, bool) {
	mu.RLock()
	defer mu.RUnlock()
	entry, ok := services[strings.ToLower(name)]
	return entry, ok
}
