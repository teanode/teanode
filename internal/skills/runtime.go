package skills

import (
	"os"
	"sync"
)

var (
	runtimeSecretsMu sync.RWMutex
	runtimeSecrets   = map[string]string{}
)

// SetRuntimeSecrets replaces runtime-resolved secrets available to skills.
func SetRuntimeSecrets(secrets map[string]string) {
	next := map[string]string{}
	for key, value := range secrets {
		next[key] = value
	}
	runtimeSecretsMu.Lock()
	runtimeSecrets = next
	runtimeSecretsMu.Unlock()
}

func resolveSecret(name string) (string, bool) {
	runtimeSecretsMu.RLock()
	value, ok := runtimeSecrets[name]
	runtimeSecretsMu.RUnlock()
	if ok && value != "" {
		return value, true
	}
	value = os.Getenv(name)
	if value != "" {
		return value, true
	}
	return "", false
}

func resolveEnv(name string) (string, bool) {
	value := os.Getenv(name)
	if value == "" {
		return "", false
	}
	return value, true
}
