package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"gopkg.in/yaml.v3"
)

// ConfigParser handles CUE validation and struct unmarshaling.
type ConfigParser struct {
	ctx *cue.Context
}

// NewConfigParser creates a new ConfigParser instance.
func NewConfigParser() *ConfigParser {
	return &ConfigParser{
		ctx: cuecontext.New(),
	}
}

// ParseAndValidate validates raw config data against a CUE schema and unmarshals into the target struct.
// - schemaSource: either embedded CUE schema string (for runtimes) or file path (for services)
// - isFile: true if schemaSource is a file path, false if it's an embedded string
// - configData: raw config (YAML or JSON)
// - target: pointer to struct to unmarshal into
func (cp *ConfigParser) ParseAndValidate(schemaSource string, isFile bool, configData []byte, target interface{}) error {
	// Load the CUE schema
	var schema *cue.Value
	var err error

	if isFile {
		schema, err = cp.loadSchemaFromFile(schemaSource)
	} else {
		schema, err = cp.loadSchemaFromString(schemaSource)
	}
	if err != nil {
		return fmt.Errorf("failed to load schema: %w", err)
	}

	// Parse config from YAML/JSON to intermediate map
	var configMap map[string]interface{}
	if err := yaml.Unmarshal(configData, &configMap); err != nil {
		// Try JSON if YAML fails
		if err := json.Unmarshal(configData, &configMap); err != nil {
			return fmt.Errorf("failed to parse config (tried YAML and JSON): %w", err)
		}
	}

	// Convert to JSON for CUE processing
	configJSON, err := json.Marshal(configMap)
	if err != nil {
		return fmt.Errorf("failed to marshal config to JSON: %w", err)
	}

	// Load config into CUE value
	configValue := cp.ctx.CompileBytes(configJSON)
	if err := configValue.Err(); err != nil {
		return fmt.Errorf("failed to parse config into CUE: %w", err)
	}

	// Unify config with schema for validation
	unified := schema.Unify(configValue)
	if err := unified.Err(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Extract validated data as JSON
	validatedJSON, err := cp.cueValueToJSON(&unified)
	if err != nil {
		return fmt.Errorf("failed to extract validated config: %w", err)
	}

	// Unmarshal into target struct using standard Go tags
	if err := json.Unmarshal(validatedJSON, target); err != nil {
		return fmt.Errorf("failed to unmarshal into target struct: %w", err)
	}

	return nil
}

// loadSchemaFromString compiles a CUE schema from an embedded string.
func (cp *ConfigParser) loadSchemaFromString(schemaStr string) (*cue.Value, error) {
	val := cp.ctx.CompileString(schemaStr)
	if err := val.Err(); err != nil {
		return nil, fmt.Errorf("failed to compile CUE schema: %w", err)
	}

	return &val, nil
}

// loadSchemaFromFile loads a CUE schema from a file.
func (cp *ConfigParser) loadSchemaFromFile(filePath string) (*cue.Value, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file: %w", err)
	}

	val := cp.ctx.CompileBytes(data, cue.Filename(filePath))
	if err := val.Err(); err != nil {
		return nil, fmt.Errorf("failed to compile CUE schema from file %s: %w", filePath, err)
	}

	return &val, nil
}

// loadSchemaFromFS loads a CUE schema from an embedded filesystem.
func (cp *ConfigParser) loadSchemaFromFS(fsys fs.FS, filePath string) (*cue.Value, error) {
	data, err := fs.ReadFile(fsys, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file from FS: %w", err)
	}

	val := cp.ctx.CompileBytes(data, cue.Filename(filePath))
	if err := val.Err(); err != nil {
		return nil, fmt.Errorf("failed to compile CUE schema from FS: %w", err)
	}

	return &val, nil
}

// cueValueToJSON converts a CUE value to JSON bytes.
func (cp *ConfigParser) cueValueToJSON(val *cue.Value) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)

	if err := val.Decode(encoder); err != nil {
		return nil, fmt.Errorf("failed to encode CUE value to JSON: %w", err)
	}

	return buf.Bytes(), nil
}
