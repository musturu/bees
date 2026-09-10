package registry

import (
	"reflect"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

// ============================================================================
// Test Structs & Services
// ============================================================================

// CustomRuntime is a test runtime configuration
type CustomRuntime struct {
	Name    string `json:"name"`
	Port    int    `json:"port"`
	MaxConn int    `json:"max_conn"`
}

// CustomService is a test service configuration
type CustomService struct {
	ServiceName string `json:"service_name"`
	Version     string `json:"version"`
	Enabled     bool   `json:"enabled"`
}

// AnotherService is another test service type
type AnotherService struct {
	HandlerType string `json:"handler_type"`
	Timeout     int    `json:"timeout"`
}

// ============================================================================
// CUE Schemas
// ============================================================================

const customRuntimeSchema = `
name: string
port: int & >0 & <65536
max_conn: int & >0
`

const customServiceSchema = `
service_name: string & !=""
version: string
enabled: bool
`

const anotherServiceSchema = `
handler_type: "auth" | "cache" | "logging"
timeout: int & >0
`

// ============================================================================
// Helper Functions
// ============================================================================

func compileCueSchema(schema string) *cue.Value {
	ctx := cuecontext.New()
	val := ctx.CompileString(schema)
	if val.Err() != nil {
		panic(val.Err())
	}
	return &val
}

func clearRegistry(t *testing.T) {
	Clear()
}

// ============================================================================
// Tests
// ============================================================================

func TestRegistryRegisterAndGet(t *testing.T) {
	clearRegistry(t)

	runtimeValidator := compileCueSchema(customRuntimeSchema)
	entry := &RegistryEntry{
		AllowedPath: "",
		Kind:        "custom_runtime",
		Schema:      runtimeValidator,
		Struct:      &CustomRuntime{},
	}

	if err := Register(entry); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	retrieved, ok := Get("custom_runtime")
	if !ok {
		t.Fatal("Get failed to retrieve registered entry")
	}

	if retrieved.Kind != "custom_runtime" {
		t.Errorf("Expected kind 'custom_runtime', got %q", retrieved.Kind)
	}
}

func TestRegistryDuplicateRegistration(t *testing.T) {
	clearRegistry(t)

	entry1 := &RegistryEntry{
		AllowedPath: "",
		Kind:        "duplicate",
		Struct:      &CustomRuntime{},
	}

	entry2 := &RegistryEntry{
		AllowedPath: "",
		Kind:        "duplicate",
		Struct:      &CustomService{},
	}

	if err := Register(entry1); err != nil {
		t.Fatalf("First registration failed: %v", err)
	}

	if err := Register(entry2); err == nil {
		t.Fatal("Expected error when registering duplicate kind, got nil")
	}
}

func TestRegistryGetByPath(t *testing.T) {
	clearRegistry(t)

	runtimeEntry := &RegistryEntry{
		AllowedPath: "runtimes",
		Kind:        "http",
		Struct:      &CustomRuntime{},
	}

	serviceEntry1 := &RegistryEntry{
		AllowedPath: "services",
		Kind:        "user_service",
		Struct:      &CustomService{},
	}

	serviceEntry2 := &RegistryEntry{
		AllowedPath: "services",
		Kind:        "auth_service",
		Struct:      &CustomService{},
	}

	if err := Register(runtimeEntry); err != nil {
		t.Fatalf("Register runtime failed: %v", err)
	}
	if err := Register(serviceEntry1); err != nil {
		t.Fatalf("Register service1 failed: %v", err)
	}
	if err := Register(serviceEntry2); err != nil {
		t.Fatalf("Register service2 failed: %v", err)
	}

	runtimeEntries := GetByPath("runtimes")
	if len(runtimeEntries) != 1 {
		t.Errorf("Expected 1 entry for 'runtimes' path, got %d", len(runtimeEntries))
	}

	serviceEntries := GetByPath("services")
	if len(serviceEntries) != 2 {
		t.Errorf("Expected 2 entries for 'services' path, got %d", len(serviceEntries))
	}
}

func TestRegistryResolveStruct(t *testing.T) {
	clearRegistry(t)

	entry := &RegistryEntry{
		AllowedPath: "",
		Kind:        "test",
		Struct:      &CustomRuntime{},
	}

	instance, err := entry.ResolveStruct()
	if err != nil {
		t.Fatalf("ResolveStruct failed: %v", err)
	}

	_, ok := instance.(*CustomRuntime)
	if !ok {
		t.Errorf("Expected *CustomRuntime, got %T", instance)
	}
}

func TestConfigParserWithYAML(t *testing.T) {
	clearRegistry(t)

	parser := NewConfigParser()
	validator := compileCueSchema(customRuntimeSchema)

	yamlConfig := `
name: test_runtime
port: 8080
max_conn: 100
`

	target := &CustomRuntime{}
	if err := parser.ParseAndUnmarshal(validator, []byte(yamlConfig), target); err != nil {
		t.Fatalf("ParseAndUnmarshal failed: %v", err)
	}

	if target.Name != "test_runtime" {
		t.Errorf("Expected name 'test_runtime', got %q", target.Name)
	}
	if target.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", target.Port)
	}
	if target.MaxConn != 100 {
		t.Errorf("Expected max_conn 100, got %d", target.MaxConn)
	}
}

func TestConfigParserWithJSON(t *testing.T) {
	clearRegistry(t)

	parser := NewConfigParser()
	validator := compileCueSchema(customServiceSchema)

	jsonConfig := `{
		"service_name": "user_api",
		"version": "1.0.0",
		"enabled": true
	}`

	target := &CustomService{}
	if err := parser.ParseAndUnmarshal(validator, []byte(jsonConfig), target); err != nil {
		t.Fatalf("ParseAndUnmarshal failed: %v", err)
	}

	if target.ServiceName != "user_api" {
		t.Errorf("Expected service_name 'user_api', got %q", target.ServiceName)
	}
	if target.Version != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got %q", target.Version)
	}
	if !target.Enabled {
		t.Errorf("Expected enabled true, got false")
	}
}

func TestConfigParserValidationFailure(t *testing.T) {
	clearRegistry(t)

	parser := NewConfigParser()
	validator := compileCueSchema(customRuntimeSchema)

	// Invalid YAML: port is out of range
	invalidConfig := `
name: test_runtime
port: 99999
max_conn: 100
`

	target := &CustomRuntime{}
	err := parser.ParseAndUnmarshal(validator, []byte(invalidConfig), target)
	if err == nil {
		t.Fatal("Expected validation error, got nil")
	}

	if err.Error() != "config validation failed: port: invalid value 99999 (out of range [0,65536))" {
		// The exact error message might vary, so just check it's a validation error
		if !contains(err.Error(), "validation failed") && !contains(err.Error(), "invalid") {
			t.Errorf("Expected validation error, got: %v", err)
		}
	}
}

func TestConfigParserWithoutValidator(t *testing.T) {
	clearRegistry(t)

	parser := NewConfigParser()

	yamlConfig := `
name: test_runtime
port: 8080
max_conn: 100
`

	target := &CustomRuntime{}
	// Pass nil as validator - should skip validation
	if err := parser.ParseAndUnmarshal(nil, []byte(yamlConfig), target); err != nil {
		t.Fatalf("ParseAndUnmarshal without validator failed: %v", err)
	}

	if target.Name != "test_runtime" {
		t.Errorf("Expected name 'test_runtime', got %q", target.Name)
	}
}

func TestIntegrationCompleteFlow(t *testing.T) {
	clearRegistry(t)

	// Register multiple entries
	runtimeValidator := compileCueSchema(customRuntimeSchema)
	serviceValidator := compileCueSchema(customServiceSchema)
	anotherValidator := compileCueSchema(anotherServiceSchema)

	runtimeEntry := &RegistryEntry{
		AllowedPath: "runtimes",
		Kind:        "http",
		Schema:      runtimeValidator,
		Struct:      &CustomRuntime{},
	}

	userServiceEntry := &RegistryEntry{
		AllowedPath: "services",
		Kind:        "user_service",
		Schema:      serviceValidator,
		Struct:      &CustomService{},
	}

	authHandlerEntry := &RegistryEntry{
		AllowedPath: "handlers",
		Kind:        "auth_handler",
		Schema:      anotherValidator,
		Struct:      &AnotherService{},
	}

	if err := Register(runtimeEntry); err != nil {
		t.Fatalf("Register runtime failed: %v", err)
	}
	if err := Register(userServiceEntry); err != nil {
		t.Fatalf("Register user_service failed: %v", err)
	}
	if err := Register(authHandlerEntry); err != nil {
		t.Fatalf("Register auth_handler failed: %v", err)
	}

	// Verify all entries
	allEntries := All()
	if len(allEntries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(allEntries))
	}

	// Parse and validate runtime config
	parser := NewConfigParser()

	runtimeConfig := `
name: production_http
port: 9090
max_conn: 500
`
	runtimeInstance, err := runtimeEntry.ResolveStruct()
	if err != nil {
		t.Fatalf("ResolveStruct failed: %v", err)
	}

	if err := parser.ParseAndUnmarshal(runtimeValidator, []byte(runtimeConfig), runtimeInstance); err != nil {
		t.Fatalf("ParseAndUnmarshal for runtime failed: %v", err)
	}

	rt := runtimeInstance.(*CustomRuntime)
	if rt.Name != "production_http" || rt.Port != 9090 {
		t.Errorf("Runtime config not parsed correctly: %+v", rt)
	}

	// Parse and validate service config
	serviceConfig := `
service_name: user_api
version: 2.0.0
enabled: true
`
	serviceInstance, err := userServiceEntry.ResolveStruct()
	if err != nil {
		t.Fatalf("ResolveStruct for service failed: %v", err)
	}

	if err := parser.ParseAndUnmarshal(serviceValidator, []byte(serviceConfig), serviceInstance); err != nil {
		t.Fatalf("ParseAndUnmarshal for service failed: %v", err)
	}

	svc := serviceInstance.(*CustomService)
	if svc.ServiceName != "user_api" || svc.Version != "2.0.0" || !svc.Enabled {
		t.Errorf("Service config not parsed correctly: %+v", svc)
	}

	// Verify path-based lookups
	runtimeEntries := GetByPath("runtimes")
	if len(runtimeEntries) != 1 {
		t.Errorf("Expected 1 runtime entry, got %d", len(runtimeEntries))
	}

	serviceEntries := GetByPath("services")
	if len(serviceEntries) != 1 {
		t.Errorf("Expected 1 service entry, got %d", len(serviceEntries))
	}

	handlerEntries := GetByPath("handlers")
	if len(handlerEntries) != 1 {
		t.Errorf("Expected 1 handler entry, got %d", len(handlerEntries))
	}
}

func TestPathSegments(t *testing.T) {
	tests := []struct {
		path     string
		expected []string
	}{
		{"", []string{}},
		{"runtimes", []string{"runtimes"}},
		{"runtimes.http", []string{"runtimes", "http"}},
		{"services.user.api", []string{"services", "user", "api"}},
	}

	for _, tt := range tests {
		result := PathSegments(tt.path)
		if !reflect.DeepEqual(result, tt.expected) {
			t.Errorf("PathSegments(%q) = %v, expected %v", tt.path, result, tt.expected)
		}
	}
}

func TestRootLevelEntry(t *testing.T) {
	clearRegistry(t)

	entry := &RegistryEntry{
		AllowedPath: "", // Root level
		Kind:        "root_config",
		Struct:      &CustomRuntime{},
	}

	if err := Register(entry); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	rootEntries := GetByPath("")
	if len(rootEntries) != 1 {
		t.Errorf("Expected 1 root entry, got %d", len(rootEntries))
	}
}

func TestMultiFormatConfigParsing(t *testing.T) {
	clearRegistry(t)

	parser := NewConfigParser()
	validator := compileCueSchema(customRuntimeSchema)

	// Test YAML with JSON fallback
	mixedYAML := `
name: test
port: 8080
max_conn: 50
`

	target1 := &CustomRuntime{}
	if err := parser.ParseAndUnmarshal(validator, []byte(mixedYAML), target1); err != nil {
		t.Fatalf("YAML parsing failed: %v", err)
	}

	// Test pure JSON
	jsonData := `{"name": "json_test", "port": 3000, "max_conn": 200}`
	target2 := &CustomRuntime{}
	if err := parser.ParseAndUnmarshal(validator, []byte(jsonData), target2); err != nil {
		t.Fatalf("JSON parsing failed: %v", err)
	}

	if target2.Port != 3000 {
		t.Errorf("Expected port 3000, got %d", target2.Port)
	}
}

func TestValidationEnumValues(t *testing.T) {
	clearRegistry(t)

	parser := NewConfigParser()
	validator := compileCueSchema(anotherServiceSchema)

	// Valid enum value
	validConfig := `
handler_type: auth
timeout: 5
`
	target := &AnotherService{}
	if err := parser.ParseAndUnmarshal(validator, []byte(validConfig), target); err != nil {
		t.Fatalf("Valid enum parsing failed: %v", err)
	}

	if target.HandlerType != "auth" {
		t.Errorf("Expected handler_type 'auth', got %q", target.HandlerType)
	}

	// Invalid enum value
	invalidConfig := `
handler_type: invalid
timeout: 5
`
	target2 := &AnotherService{}
	err := parser.ParseAndUnmarshal(validator, []byte(invalidConfig), target2)
	if err == nil {
		t.Fatal("Expected validation error for invalid enum value")
	}
}

// ============================================================================
// Helper Functions
// ============================================================================

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || index(s, substr) >= 0))
}

func index(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
