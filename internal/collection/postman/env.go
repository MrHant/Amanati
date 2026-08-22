package postman

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrhant/amanati/internal/collection"
)

// An exported environment is its own document, with nothing in it naming the
// collection that uses it:
//
//	{"name": "staging", "values": [{"key": "host", "value": "…", "enabled": true}],
//	 "_postman_variable_scope": "environment"}

// envDoc is an exported environment or globals document.
type envDoc struct {
	Name   string     `json:"name"`
	Values []envValue `json:"values"`
	Scope  string     `json:"_postman_variable_scope"`
}

// envValue is a kv with the opposite polarity: environments spell the flag as
// "enabled", and its absence means enabled. Some exporters write "disabled"
// here anyway, so both are read.
type envValue struct {
	Key      string    `json:"key"`
	Value    loose     `json:"value"`
	Enabled  *flexBool `json:"enabled"`
	Disabled flexBool  `json:"disabled"`
}

func (v envValue) off() bool {
	if bool(v.Disabled) {
		return true
	}
	return v.Enabled != nil && !bool(*v.Enabled)
}

// envProbe is the minimum needed to tell an environment from a collection.
type envProbe struct {
	Values json.RawMessage `json:"values"`
	Item   json.RawMessage `json:"item"`
	Info   json.RawMessage `json:"info"`
	Scope  string          `json:"_postman_variable_scope"`
}

// maxEnvSize caps what AcceptsEnv will parse. It has to read the whole
// document — "_postman_variable_scope" is written after the values, so no
// fixed-size peek can find it — while running over every file next to an
// imported collection.
const maxEnvSize = 4 << 20

func (Importer) EnvPatterns() []string {
	return []string{"*.postman_environment.json", "*.json"}
}

// AcceptsEnv reports whether path is an exported environment.
func (Importer) AcceptsEnv(path string) bool {
	if !strings.EqualFold(filepath.Ext(path), ".json") {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > maxEnvSize {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var probe envProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	if probe.Item != nil || probe.Info != nil {
		return false // a collection, not an environment
	}
	if probe.Scope != "" {
		return probe.Scope == "environment"
	}
	// Older exports omit the scope marker; a bare list of values is enough.
	return probe.Values != nil
}

// ImportEnv reads one exported environment file.
func (Importer) ImportEnv(path string) (collection.Environment, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return collection.Environment{}, err
	}
	var doc envDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return collection.Environment{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if doc.Scope != "" && doc.Scope != "environment" {
		return collection.Environment{}, fmt.Errorf("%s is a %q export, not an environment", filepath.Base(path), doc.Scope)
	}

	env := collection.Environment{Name: strings.TrimSpace(doc.Name), Source: path}
	if env.Name == "" {
		env.Name = envNameFromFile(path)
	}
	for _, v := range doc.Values {
		if strings.TrimSpace(v.Key) == "" {
			continue
		}
		env.Variables = append(env.Variables, collection.Variable{
			Key:      v.Key,
			Value:    v.Value.String(),
			Disabled: v.off(),
		})
	}
	return env, nil
}

// envNameFromFile falls back to the file name for an export with no name,
// dropping Postman's ".postman_environment" tag along with the extension.
func envNameFromFile(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name = strings.TrimSuffix(name, ".postman_environment")
	if name == "" {
		return "Environment"
	}
	return name
}
