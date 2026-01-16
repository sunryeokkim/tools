package lint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	schemaUnknown = "unknown"
	schemaBool    = "bool"
	schemaNumber  = "number"
	schemaString  = "string"
	schemaArray   = "array"
	schemaObject  = "object"
)

type SchemaEntry struct {
	Key               string
	ValueType         string
	DefaultValue      string
	KnownOnly         bool
	Deprecated        bool
	DeprecatedMessage string
}

type Schema struct {
	Entries map[string]SchemaEntry
}

func LoadDatadogSchema(repoRoot string) (Schema, error) {
	configDir := filepath.Join(repoRoot, "pkg", "config", "setup")
	fs := token.NewFileSet()
	files, err := os.ReadDir(configDir)
	if err != nil {
		return Schema{}, fmt.Errorf("read config setup dir: %w", err)
	}

	entries := map[string]SchemaEntry{}
	for _, entry := range files {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(configDir, name)
		file, err := parser.ParseFile(fs, path, nil, parser.ParseComments)
		if err != nil {
			return Schema{}, fmt.Errorf("parse %s: %w", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
				switch selector.Sel.Name {
				case "BindEnvAndSetDefault":
					entry, ok := parseSchemaCall(call, false)
					if ok {
						entries[entry.Key] = entry
					}
				case "SetDefault":
					entry, ok := parseSchemaCall(call, false)
					if ok {
						entries[entry.Key] = entry
					}
				case "SetKnown":
					entry, ok := parseSchemaCall(call, true)
					if ok {
						entries[entry.Key] = entry
					}
				case "BindEnv":
					entry, ok := parseSchemaCall(call, true)
					if ok {
						entries[entry.Key] = entry
					}
				}
				return true
			}

			if ident, ok := call.Fun.(*ast.Ident); ok {
				switch ident.Name {
				case "procBindEnvAndSetDefault":
					entry, ok := parseSchemaCallAtIndex(call, 1, false)
					if ok {
						entries[entry.Key] = entry
					}
				case "procBindEnv":
					entry, ok := parseSchemaCallAtIndex(call, 1, true)
					if ok {
						entries[entry.Key] = entry
					}
				}
			}
			return true
		})
	}

	if err := addTemplateSchema(repoRoot, entries); err != nil {
		return Schema{}, err
	}

	return Schema{Entries: entries}, nil
}

func parseSchemaCall(call *ast.CallExpr, knownOnly bool) (SchemaEntry, bool) {
	return parseSchemaCallAtIndex(call, 0, knownOnly)
}

func parseSchemaCallAtIndex(call *ast.CallExpr, keyIndex int, knownOnly bool) (SchemaEntry, bool) {
	if len(call.Args) <= keyIndex {
		return SchemaEntry{}, false
	}

	key, ok := extractStringLiteral(call.Args[keyIndex])
	if !ok {
		return SchemaEntry{}, false
	}

	entry := SchemaEntry{Key: key, KnownOnly: knownOnly}
	if len(call.Args) > keyIndex+1 && !knownOnly {
		typeName, defaultValue := inferValue(call.Args[keyIndex+1])
		entry.ValueType = typeName
		entry.DefaultValue = defaultValue
	} else {
		entry.ValueType = schemaUnknown
	}

	return entry, true
}

func extractStringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func inferValue(expr ast.Expr) (string, string) {
	switch v := expr.(type) {
	case *ast.BasicLit:
		switch v.Kind {
		case token.STRING:
			value, _ := strconv.Unquote(v.Value)
			return schemaString, value
		case token.INT, token.FLOAT:
			return schemaNumber, v.Value
		}
	case *ast.Ident:
		if v.Name == "true" || v.Name == "false" {
			return schemaBool, v.Name
		}
	case *ast.CompositeLit:
		switch v.Type.(type) {
		case *ast.ArrayType:
			return schemaArray, "[]"
		case *ast.MapType:
			return schemaObject, "{}"
		}
	case *ast.CallExpr:
		if isStringReturningCall(v) {
			return schemaString, ""
		}
		return schemaUnknown, ""
	case *ast.BinaryExpr:
		if containsTimeSelector(v) {
			return schemaNumber, ""
		}
	}

	return schemaUnknown, ""
}

func isStringReturningCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if ident, ok := selector.X.(*ast.Ident); ok {
		if ident.Name == "filepath" && selector.Sel.Name == "Join" {
			return true
		}
		if ident.Name == "strings" && strings.HasPrefix(selector.Sel.Name, "To") {
			return true
		}
	}
	return false
}

func containsTimeSelector(expr ast.Expr) bool {
	switch v := expr.(type) {
	case *ast.SelectorExpr:
		if ident, ok := v.X.(*ast.Ident); ok && ident.Name == "time" {
			return true
		}
	case *ast.BinaryExpr:
		return containsTimeSelector(v.X) || containsTimeSelector(v.Y)
	}
	return false
}

func addTemplateSchema(repoRoot string, entries map[string]SchemaEntry) error {
	templatePath := filepath.Join(repoRoot, "pkg", "config", "config_template.yaml")
	content, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read config_template.yaml: %w", err)
	}

	addTemplateParamSchema(entries, content)

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		sanitized := sanitizeTemplateYAML(content)
		if err := yaml.Unmarshal(sanitized, &root); err != nil {
			return nil
		}
	}
	if len(root.Content) == 0 {
		return nil
	}

	addTemplateNode(entries, root.Content[0], nil)
	return nil
}

func addTemplateParamSchema(entries map[string]SchemaEntry, content []byte) {
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "## @param ") {
			continue
		}
		payload := strings.TrimPrefix(trimmed, "## @param ")
		parts := strings.SplitN(payload, " - ", 3)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		typeName := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		valueType, ok := schemaTypeFromTemplate(typeName)
		if !ok {
			continue
		}
		entry, exists := entries[key]
		if !exists || entry.ValueType == schemaUnknown {
			entry.Key = key
			entry.ValueType = valueType
		}
		if !entry.Deprecated {
			if message, ok := deprecatedCommentMessage(trimmed); ok {
				entry.Deprecated = true
				entry.DeprecatedMessage = message
			}
		}
		entries[key] = entry
	}
}

func schemaTypeFromTemplate(typeName string) (string, bool) {
	switch strings.ToLower(typeName) {
	case "string":
		return schemaString, true
	case "boolean":
		return schemaBool, true
	case "integer", "float", "number", "duration":
		return schemaNumber, true
	case "list":
		return schemaArray, true
	case "map", "custom object", "object":
		return schemaObject, true
	default:
		return schemaUnknown, false
	}
}

func addTemplateNode(entries map[string]SchemaEntry, node *yaml.Node, path []string) {
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]
			next := append(path, keyNode.Value)
			keyPath := strings.Join(next, ".")
			if entry, ok := entries[keyPath]; ok {
				if !entry.Deprecated {
					if message, ok := deprecatedCommentMessage(commentText(keyNode, valueNode)); ok {
						entry.Deprecated = true
						entry.DeprecatedMessage = message
						entries[keyPath] = entry
					}
				}
			} else {
				entry := SchemaEntry{
					Key:       keyPath,
					ValueType: YAMLNodeType(valueNode),
				}
				if message, ok := deprecatedCommentMessage(commentText(keyNode, valueNode)); ok {
					entry.Deprecated = true
					entry.DeprecatedMessage = message
				}
				entries[keyPath] = entry
			}
			addTemplateNode(entries, valueNode, next)
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			addTemplateNode(entries, child, path)
		}
	}
}

func commentText(nodes ...*yaml.Node) string {
	parts := []string{}
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if node.HeadComment != "" {
			parts = append(parts, node.HeadComment)
		}
		if node.LineComment != "" {
			parts = append(parts, node.LineComment)
		}
		if node.FootComment != "" {
			parts = append(parts, node.FootComment)
		}
	}
	return strings.Join(parts, "\n")
}

func deprecatedCommentMessage(comment string) (string, bool) {
	if comment == "" {
		return "", false
	}
	lines := strings.Split(comment, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(strings.ToLower(trimmed), "deprecated") {
			return trimmed, true
		}
	}
	return "", false
}

func sanitizeTemplateYAML(content []byte) []byte {
	lines := strings.Split(string(content), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, "{{") || strings.Contains(line, "}}") || strings.Contains(line, "{%") || strings.Contains(line, "%}") {
			continue
		}
		filtered = append(filtered, line)
	}
	return []byte(strings.Join(filtered, "\n"))
}

