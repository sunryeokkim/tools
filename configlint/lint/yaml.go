package lint

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type YamlDocument struct {
	Root      *yaml.Node
	Positions map[string]Position
}

func ParseYAMLWithPositions(content []byte) (YamlDocument, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return YamlDocument{}, err
	}
	positions := map[string]Position{}
	if len(root.Content) > 0 {
		buildPathIndex(root.Content[0], nil, positions)
	}
	return YamlDocument{Root: &root, Positions: positions}, nil
}

func buildPathIndex(node *yaml.Node, path []string, positions map[string]Position) {
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			value := node.Content[i+1]
			keyPath := append(path, key.Value)
			positions[strings.Join(keyPath, ".")] = Position{Line: key.Line, Column: key.Column}
			buildPathIndex(value, keyPath, positions)
		}
	case yaml.SequenceNode:
		for idx, child := range node.Content {
			seqPath := append(path, fmt.Sprintf("[%d]", idx))
			buildPathIndex(child, seqPath, positions)
		}
	case yaml.ScalarNode:
		if len(path) == 0 {
			return
		}
		positions[strings.Join(path, ".")] = Position{Line: node.Line, Column: node.Column}
	}
}

func YAMLNodeType(node *yaml.Node) string {
	if node == nil {
		return schemaUnknown
	}
	switch node.Kind {
	case yaml.MappingNode:
		return schemaObject
	case yaml.SequenceNode:
		return schemaArray
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!str":
			return schemaString
		case "!!bool":
			return schemaBool
		case "!!int", "!!float":
			return schemaNumber
		default:
			return schemaString
		}
	}
	return schemaUnknown
}

func LookupPosition(positions map[string]Position, path string) Position {
	if pos, ok := positions[path]; ok {
		return pos
	}
	return Position{}
}

