package commandmcp

import (
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// OpenHands drops items/properties for nullable type arrays. Optional fields can
// be omitted instead; advertise concrete types without duplicating domain types.
func concreteToolSchema(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}
	if len(schema.Types) == 2 {
		for index, kind := range schema.Types {
			if kind == "null" {
				schema.Type = schema.Types[1-index]
				schema.Types = nil
				break
			}
		}
	}
	for _, property := range schema.Properties {
		concreteToolSchema(property)
	}
	concreteToolSchema(schema.Items)
	concreteToolSchema(schema.AdditionalProperties)
}

func stringField(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
func integerField(max int) map[string]any {
	return map[string]any{"type": "integer", "minimum": 1, "maximum": max}
}
func commandMCPTool(name, description string, properties map[string]any, required ...string) *mcp.Tool {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return &mcp.Tool{Name: name, Description: description, InputSchema: schema}
}
