package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

const systemSettingsYAMLKey = "system_settings"

var configFileMu sync.Mutex

// UpdateSystemSettings merges settings into the local config.yaml mirror.
// It is a no-op for callers (mostly tests and offline helpers) that have not
// initialized the application config paths.
func UpdateSystemSettings(values map[string]string) error {
	if len(values) == 0 || AppPaths.ConfigFile == "" {
		return nil
	}

	configFileMu.Lock()
	defer configFileMu.Unlock()

	doc, err := readConfigDocument()
	if err != nil {
		return err
	}

	settings, err := settingsFromDocument(doc)
	if err != nil {
		return err
	}
	for key, value := range values {
		settings[key] = value
	}

	return writeSystemSettingsDocument(doc, settings)
}

// ReplaceSystemSettings rewrites the complete local settings mirror. It is
// used after startup reconciliation and settings restores so stale keys do not
// survive in config.yaml.
func ReplaceSystemSettings(values map[string]string) error {
	if AppPaths.ConfigFile == "" {
		return nil
	}

	configFileMu.Lock()
	defer configFileMu.Unlock()

	doc, err := readConfigDocument()
	if err != nil {
		return err
	}

	settings := make(map[string]string, len(values))
	for key, value := range values {
		settings[key] = value
	}
	return writeSystemSettingsDocument(doc, settings)
}

func readConfigDocument() (*yaml.Node, error) {
	configPath := filepath.Clean(AppPaths.ConfigFile)
	data, err := os.ReadFile(configPath) //nolint:gosec // application-controlled config path
	if err != nil {
		return nil, fmt.Errorf("read config mirror: %w", err)
	}

	doc := &yaml.Node{}
	if err := yaml.Unmarshal(data, doc); err != nil {
		return nil, fmt.Errorf("parse config mirror: %w", err)
	}
	if len(doc.Content) == 0 {
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config mirror root must be a YAML mapping")
	}
	return doc, nil
}

func settingsFromDocument(doc *yaml.Node) (map[string]string, error) {
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != systemSettingsYAMLKey {
			continue
		}
		settings := map[string]string{}
		if err := root.Content[i+1].Decode(&settings); err != nil {
			return nil, fmt.Errorf("decode %s: %w", systemSettingsYAMLKey, err)
		}
		return settings, nil
	}
	return map[string]string{}, nil
}

func writeSystemSettingsDocument(doc *yaml.Node, settings map[string]string) error {
	root := doc.Content[0]
	settingsNode := buildSettingsNode(settings)
	found := false
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == systemSettingsYAMLKey {
			root.Content[i+1] = settingsNode
			found = true
			break
		}
	}
	if !found {
		keyNode := &yaml.Node{
			Kind:        yaml.ScalarNode,
			Tag:         "!!str",
			Value:       systemSettingsYAMLKey,
			HeadComment: "Web settings mirror. May contain service passwords and API keys.",
		}
		root.Content = append(root.Content, keyNode, settingsNode)
	}

	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return fmt.Errorf("encode config mirror: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("close config encoder: %w", err)
	}

	if err := writePrivateConfigFile(output.Bytes()); err != nil {
		return err
	}
	if AppConfig != nil {
		AppConfig.SystemSettings = cloneSettings(settings)
	}
	return nil
}

func buildSettingsNode(settings map[string]string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: settings[key]},
		)
	}
	return node
}

func writePrivateConfigFile(data []byte) error {
	configPath := filepath.Clean(AppPaths.ConfigFile)
	temp, err := os.CreateTemp(filepath.Dir(configPath), ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary config mirror: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secure temporary config mirror: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary config mirror: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync temporary config mirror: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary config mirror: %w", err)
	}

	if err := os.Rename(tempPath, configPath); err != nil {
		// Windows cannot always replace an existing destination with Rename.
		// Fall back to a direct protected write while retaining the temp file
		// until the write has completed.
		if writeErr := os.WriteFile(configPath, data, 0o600); writeErr != nil {
			return fmt.Errorf("replace config mirror: %w (fallback: %v)", err, writeErr)
		}
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		return fmt.Errorf("secure config mirror: %w", err)
	}
	return nil
}

func cloneSettings(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
