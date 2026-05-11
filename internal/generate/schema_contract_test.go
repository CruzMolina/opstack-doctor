package generate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

func TestSchemaContractValidatesExampleConfig(t *testing.T) {
	schema := compileExampleSchema(t)
	doc := loadYAMLAsJSON(t, "../../examples/doctor.example.yaml")
	if err := schema.Validate(doc); err != nil {
		t.Fatalf("examples/doctor.example.yaml should validate against examples/doctor.schema.json: %v", err)
	}
}

func TestSchemaContractRejectsInvalidFixtures(t *testing.T) {
	schema := compileExampleSchema(t)
	fixtures := []string{
		"missing-required.yaml",
		"invalid-role.yaml",
		"invalid-url-scheme.yaml",
		"unexpected-property.yaml",
	}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			doc := loadYAMLAsJSON(t, filepath.Join("testdata", "schema", fixture))
			if err := schema.Validate(doc); err == nil {
				t.Fatalf("%s unexpectedly validated against examples/doctor.schema.json", fixture)
			}
		})
	}
}

func compileExampleSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	data, err := os.ReadFile("../../examples/doctor.schema.json")
	if err != nil {
		t.Fatalf("read example schema: %v", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse example schema JSON: %v", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("doctor.schema.json", doc); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile("doctor.schema.json")
	if err != nil {
		t.Fatalf("compile example schema: %v", err)
	}
	return schema
}

func loadYAMLAsJSON(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read YAML %s: %v", path, err)
	}
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse YAML %s: %v", path, err)
	}
	compatible, err := jsonCompatible(raw)
	if err != nil {
		t.Fatalf("convert YAML %s to JSON-compatible value: %v", path, err)
	}
	jsonData, err := json.Marshal(compatible)
	if err != nil {
		t.Fatalf("marshal JSON-compatible YAML %s: %v", path, err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(jsonData))
	if err != nil {
		t.Fatalf("parse normalized JSON for %s: %v", path, err)
	}
	return doc
}

func jsonCompatible(value any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			converted, err := jsonCompatible(child)
			if err != nil {
				return nil, err
			}
			out[key] = converted
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			keyString, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("non-string YAML key %v", key)
			}
			converted, err := jsonCompatible(child)
			if err != nil {
				return nil, err
			}
			out[keyString] = converted
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(v))
		for _, child := range v {
			converted, err := jsonCompatible(child)
			if err != nil {
				return nil, err
			}
			out = append(out, converted)
		}
		return out, nil
	default:
		return value, nil
	}
}
