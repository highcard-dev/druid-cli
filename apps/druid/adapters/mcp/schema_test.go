package commandmcp

import (
	"github.com/google/jsonschema-go/jsonschema"
	"strings"
	"testing"
)

func TestPublishUIDescriptionRequiresSeparateCustomerApproval(t *testing.T) {
	definition := publishUITool()
	for _, instruction := range []string{"focused previews", "end your turn and wait", "subsequent explicit customer reply", "original UI-change request is not approval", "Revisions require renewed approval", "failed previews", "behavior-only changes", "Do not bypass approval", "not a server-enforced approval check"} {
		if !strings.Contains(definition.Description, instruction) {
			t.Errorf("publish_ui description missing %q", instruction)
		}
	}
}

func TestPublishUISchemaUsesDeclaredArtifactsAndOptionalBuild(t *testing.T) {
	properties := publishUITool().InputSchema.(map[string]any)["properties"].(map[string]any)
	if len(properties) != 2 || properties["scope"] == nil || properties["with_build"] == nil || properties["path"] != nil {
		t.Fatalf("unexpected publication schema: %#v", properties)
	}
}

func TestCommandWaitSchemaUsesOnlyCommandNameAndTimeout(t *testing.T) {
	tool := waitForCommandTool()
	schema := tool.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	required := schema["required"].([]string)
	if len(properties) != 2 || properties["command"] == nil || properties["timeout_seconds"] == nil || len(required) != 1 || required[0] != "command" {
		t.Fatal("command waiting requires more than a name and optional timeout")
	}
}

func TestConcreteToolSchemaNormalizesMapValuesWithoutChangingRequiredFields(t *testing.T) {
	schema := &jsonschema.Schema{Type: "object", Required: []string{"required"}, Properties: map[string]*jsonschema.Schema{
		"required": {Types: []string{"string", "null"}},
		"optional": {Type: "object", AdditionalProperties: &jsonschema.Schema{Types: []string{"null", "string"}}},
	}}
	concreteToolSchema(schema)
	if schema.Properties["required"].Type != "string" || schema.Properties["optional"].AdditionalProperties.Type != "string" {
		t.Fatal("nullable scalar or map value lost its concrete type")
	}
	if len(schema.Required) != 1 || schema.Required[0] != "required" {
		t.Fatal("schema normalization changed field optionality")
	}
}

func TestCreateCommandSchemaPreservesNestedTypesForOpenHands(t *testing.T) {
	definition := createCommandTool()
	if definition.Name != "create_command" {
		t.Fatal("creation tool missing")
	}
	instructions := definition.InputSchema.(map[string]any)["properties"].(map[string]any)["instructions"].(*jsonschema.Schema)
	var visit func(*jsonschema.Schema)
	visit = func(schema *jsonschema.Schema) {
		if schema == nil {
			return
		}
		if len(schema.Types) > 0 {
			t.Fatalf("OpenHands loses nested fields for nullable type %v", schema.Types)
		}
		for _, property := range schema.Properties {
			visit(property)
		}
		visit(schema.Items)
		visit(schema.AdditionalProperties)
	}
	visit(instructions)
	procedures := instructions.Properties["procedures"]
	if procedures.Type != "array" || procedures.MinItems == nil || *procedures.MinItems != 1 || procedures.Items.Type != "object" {
		t.Fatal("procedures must advertise a nonempty object array")
	}
	procedure := procedures.Items
	for _, field := range []string{"image", "id", "command", "env", "mounts", "expectedPorts", "target", "signal"} {
		if procedure.Properties[field] == nil {
			t.Fatalf("procedure field %s missing", field)
		}
		if procedure.Properties[field].Description == "" {
			t.Fatalf("Scroll field %s lost its description", field)
		}
	}
	if procedure.Properties["command"].Type != "array" || procedure.Properties["command"].Items.Type != "string" || procedure.Properties["env"].AdditionalProperties.Type != "string" {
		t.Fatal("argument array or environment map schema lost")
	}
	if procedure.Properties["mounts"].Items.Properties["read_only"].Type != "boolean" || procedure.Properties["expectedPorts"].Items.Properties["name"].Type != "string" {
		t.Fatal("mount or port-reference schema lost")
	}
	if instructions.Properties["identity"] != nil || len(procedure.Required) != 0 || len(instructions.Properties["run"].Enum) != 4 {
		t.Fatal("identity exclusion, optional ignore_failure, or run modes changed")
	}
	if procedures.Description == "" || !strings.Contains(instructions.Properties["run"].Description, "NOT a shell command") {
		t.Fatal("Scroll procedure or scheduling-mode documentation lost")
	}
	if !strings.Contains(definition.Description, "Example: ") {
		t.Fatal("model-visible example missing")
	}
}
