package lint

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type IntegrationSchema struct {
	InitConfig Schema
	Instances  Schema
}

func LoadIntegrationSchemas(paths []string) (map[string]IntegrationSchema, error) {
	schemas := map[string]IntegrationSchema{}
	for _, repoPath := range paths {
		if repoPath == "" {
			continue
		}
		info, err := os.Stat(repoPath)
		if err != nil {
			return nil, fmt.Errorf("stat integrations repo %s: %w", repoPath, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("integrations repo %s is not a directory", repoPath)
		}
		if err := loadIntegrationSchemasFromRepo(repoPath, schemas); err != nil {
			return nil, err
		}
	}
	return schemas, nil
}

func loadIntegrationSchemasFromRepo(repoPath string, schemas map[string]IntegrationSchema) error {
	return filepath.WalkDir(repoPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if base != "conf.yaml.example" && base != "conf.yaml" {
			return nil
		}
		schemaName := filepath.Base(filepath.Dir(path))
		if schemaName == "." || schemaName == string(filepath.Separator) {
			return nil
		}
		configSchema, err := parseIntegrationTemplate(path)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if existing, ok := schemas[schemaName]; ok {
			configSchema = mergeIntegrationSchemas(existing, configSchema)
		}
		schemas[schemaName] = configSchema
		return nil
	})
}

func parseIntegrationTemplate(path string) (IntegrationSchema, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return IntegrationSchema{}, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return IntegrationSchema{}, err
	}
	if len(root.Content) == 0 {
		return IntegrationSchema{}, nil
	}
	doc := root.Content[0]
	initSchema := Schema{Entries: map[string]SchemaEntry{}}
	instanceSchema := Schema{Entries: map[string]SchemaEntry{}}
	initNode := lookupTopLevel(doc, "init_config")
	if initNode != nil {
		addTemplateSchemaEntries(initSchema.Entries, initNode, nil)
	}
	instancesNode := lookupTopLevel(doc, "instances")
	if instancesNode != nil && instancesNode.Kind == yaml.SequenceNode {
		for _, child := range instancesNode.Content {
			addTemplateSchemaEntries(instanceSchema.Entries, child, nil)
		}
	}
	return IntegrationSchema{
		InitConfig: initSchema,
		Instances:  instanceSchema,
	}, nil
}

func mergeIntegrationSchemas(a IntegrationSchema, b IntegrationSchema) IntegrationSchema {
	for key, entry := range b.InitConfig.Entries {
		if _, ok := a.InitConfig.Entries[key]; !ok {
			a.InitConfig.Entries[key] = entry
		}
	}
	for key, entry := range b.Instances.Entries {
		if _, ok := a.Instances.Entries[key]; !ok {
			a.Instances.Entries[key] = entry
		}
	}
	return a
}

func addTemplateSchemaEntries(entries map[string]SchemaEntry, node *yaml.Node, path []string) {
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]
			next := append(path, keyNode.Value)
			keyPath := strings.Join(next, ".")
			if _, ok := entries[keyPath]; !ok {
				entries[keyPath] = SchemaEntry{
					Key:       keyPath,
					ValueType: YAMLNodeType(valueNode),
				}
			}
			addTemplateSchemaEntries(entries, valueNode, next)
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			addTemplateSchemaEntries(entries, child, path)
		}
	}
}

