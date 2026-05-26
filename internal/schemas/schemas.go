// Package schemas exposes shared JSON schema definitions.
package schemas

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed *.json
var schemaFiles embed.FS

var (
	configSchemaJson  = mustReadSchemaFile("config.schema.json")
	agentSchemaJson   = mustReadSchemaFile("agentConfig.schema.json")
	userSchemaJson    = mustReadSchemaFile("userConfig.schema.json")
	projectSchemaJson = mustReadSchemaFile("projectConfig.schema.json")
)

func mustReadSchemaFile(path string) []byte {
	data, err := schemaFiles.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("schemas: loading embedded schema %s: %v", path, err))
	}
	return data
}

func ConfigSchema() json.RawMessage {
	return json.RawMessage(configSchemaJson)
}

func AgentSchema() json.RawMessage {
	return json.RawMessage(agentSchemaJson)
}

func UserSchema() json.RawMessage {
	return json.RawMessage(userSchemaJson)
}

func ProjectSchema() json.RawMessage {
	return json.RawMessage(projectSchemaJson)
}
