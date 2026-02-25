package configurations

import (
	"encoding/json"
	"strconv"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
)

func ResolveConfiguration(configuration *models.Configuration, options store.ResolveConfigurationOptions) (*models.Configuration, error) {
	resolved := &models.Configuration{}
	if configuration != nil {
		encoded, err := json.Marshal(configuration)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(encoded, resolved); err != nil {
			return nil, err
		}
	}

	if options.ApplySchemaDefaults != nil && *options.ApplySchemaDefaults {
		applyDefaults(resolved)
	}
	applyEnvironment(resolved, options.Environment)
	applyCLIFlags(resolved, options.CLIFlags)
	return resolved, nil
}

func applyDefaults(configuration *models.Configuration) {
	if configuration.Gateway == nil {
		configuration.Gateway = &models.GatewayConfiguration{}
	}
	if configuration.Gateway.Port == nil {
		defaultPort := 8833
		configuration.Gateway.Port = &defaultPort
	}
	if configuration.Gateway.Bind == nil {
		defaultBind := "loopback"
		configuration.Gateway.Bind = &defaultBind
	}
}

func applyEnvironment(configuration *models.Configuration, environment *map[string]string) {
	if environment == nil {
		return
	}
	if value, ok := (*environment)["TEANODE_GATEWAY_PORT"]; ok {
		if port, err := strconv.Atoi(value); err == nil {
			setGatewayPort(configuration, port)
		}
	}
	if value, ok := (*environment)["TEANODE_GATEWAY_BIND"]; ok {
		setGatewayBind(configuration, value)
	}
}

func applyCLIFlags(configuration *models.Configuration, cliFlags *map[string]string) {
	if cliFlags == nil {
		return
	}
	if value, ok := (*cliFlags)["port"]; ok {
		if port, err := strconv.Atoi(value); err == nil {
			setGatewayPort(configuration, port)
		}
	}
	if value, ok := (*cliFlags)["bind"]; ok {
		setGatewayBind(configuration, value)
	}
}

func setGatewayPort(configuration *models.Configuration, port int) {
	if configuration.Gateway == nil {
		configuration.Gateway = &models.GatewayConfiguration{}
	}
	configuration.Gateway.Port = &port
}

func setGatewayBind(configuration *models.Configuration, bind string) {
	if configuration.Gateway == nil {
		configuration.Gateway = &models.GatewayConfiguration{}
	}
	configuration.Gateway.Bind = &bind
}
