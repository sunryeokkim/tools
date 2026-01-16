package lint

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func (l *Linter) lintDatadog(path string, doc YamlDocument) []Finding {
	findings := []Finding{}
	nodes := map[string]*NodeWithPath{}
	if len(doc.Root.Content) == 0 {
		return findings
	}
	collectMappingNodes(doc.Root.Content[0], "", nodes)
	prefixes := schemaPrefixes(l.Schema)

	for key, node := range nodes {
		if !schemaKeyKnown(l.Schema, prefixes, key) {
			if schemaAllowsNested(l.Schema, key) {
				continue
			}
			pos := LookupPosition(doc.Positions, key)
			findings = append(findings, Finding{
				RuleID:   "DDYAML002",
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("unknown configuration key: %s", key),
				File:     path,
				Path:     key,
				Position: pos,
			})
			continue
		}

		entry, ok := l.Schema.Entries[key]
		if !ok || entry.ValueType == schemaUnknown {
			continue
		}
		if entry.Deprecated {
			pos := LookupPosition(doc.Positions, key)
			message := "configuration key is deprecated"
			if entry.DeprecatedMessage != "" {
				message = entry.DeprecatedMessage
			}
			findings = append(findings, Finding{
				RuleID:   "DDYAML014",
				Severity: SeverityWarn,
				Message:  message,
				File:     path,
				Path:     key,
				Position: pos,
			})
		}
		nodeType := YAMLNodeType(node.Node)
		if entry.ValueType != nodeType {
			pos := LookupPosition(doc.Positions, key)
			findings = append(findings, Finding{
				RuleID:   "DDYAML003",
				Severity: SeverityError,
				Message:  fmt.Sprintf("type mismatch for %s: expected %s, got %s", key, entry.ValueType, nodeType),
				File:     path,
				Path:     key,
				Position: pos,
			})
		}
	}

	findings = append(findings, l.lintDatadogRules(path, doc, nodes)...)
	return findings
}

func (l *Linter) lintDatadogRules(path string, doc YamlDocument, nodes map[string]*NodeWithPath) []Finding {
	findings := []Finding{}

	if node := getNode(nodes, "disable_unsafe_yaml"); node != nil {
		if node.Node.Tag == "!!bool" && node.Node.Value == "false" {
			pos := LookupPosition(doc.Positions, "disable_unsafe_yaml")
			findings = append(findings, Finding{
				RuleID:   "DDYAML004",
				Severity: SeverityWarn,
				Message:  "disable_unsafe_yaml is false; enable safe YAML parsing",
				File:     path,
				Path:     "disable_unsafe_yaml",
				Position: pos,
			})
		}
	}

	if node := getNode(nodes, "secret_backend_command"); node != nil {
		if getNode(nodes, "secret_backend_type") == nil && node.Node.Value != "" {
			pos := LookupPosition(doc.Positions, "secret_backend_command")
			findings = append(findings, Finding{
				RuleID:   "DDYAML006",
				Severity: SeverityWarn,
				Message:  "secret_backend_command is set but secret_backend_type is missing",
				File:     path,
				Path:     "secret_backend_command",
				Position: pos,
			})
		}
	}

	if node := getNode(nodes, "secret_backend_arguments"); node != nil {
		if node.Node.Kind != yaml.SequenceNode {
			pos := LookupPosition(doc.Positions, "secret_backend_arguments")
			findings = append(findings, Finding{
				RuleID:   "DDYAML007",
				Severity: SeverityError,
				Message:  "secret_backend_arguments must be a list",
				File:     path,
				Path:     "secret_backend_arguments",
				Position: pos,
			})
		}
	}

	if node := getNode(nodes, "kubernetes_resources_annotations_as_tags"); node != nil {
		if node.Node.Kind != yaml.MappingNode {
			pos := LookupPosition(doc.Positions, "kubernetes_resources_annotations_as_tags")
			findings = append(findings, Finding{
				RuleID:   "DDYAML008",
				Severity: SeverityError,
				Message:  "kubernetes_resources_annotations_as_tags should be a map",
				File:     path,
				Path:     "kubernetes_resources_annotations_as_tags",
				Position: pos,
			})
		}
	}

	if node := getNode(nodes, "kubernetes_resources_labels_as_tags"); node != nil {
		if node.Node.Kind != yaml.MappingNode {
			pos := LookupPosition(doc.Positions, "kubernetes_resources_labels_as_tags")
			findings = append(findings, Finding{
				RuleID:   "DDYAML009",
				Severity: SeverityError,
				Message:  "kubernetes_resources_labels_as_tags should be a map",
				File:     path,
				Path:     "kubernetes_resources_labels_as_tags",
				Position: pos,
			})
		}
	}

	if node := getNode(nodes, "site"); node != nil {
		if node.Node.Tag == "!!str" {
			site := strings.TrimSpace(node.Node.Value)
			if site != "" && !isKnownDatadogSite(site) {
				pos := LookupPosition(doc.Positions, "site")
				findings = append(findings, Finding{
					RuleID:   "DDYAML013",
					Severity: SeverityWarn,
					Message:  fmt.Sprintf("site %q is not a recognized Datadog site", site),
					File:     path,
					Path:     "site",
					Position: pos,
				})
			}
		}
	}

	return findings
}

func isKnownDatadogSite(site string) bool {
	switch site {
	case "datadoghq.com", "datadoghq.eu", "us3.datadoghq.com", "us5.datadoghq.com", "ap1.datadoghq.com", "ddog-gov.com":
		return true
	default:
		return false
	}
}

func (l *Linter) lintIntegration(path string, doc YamlDocument) []Finding {
	findings := []Finding{}
	if len(doc.Root.Content) == 0 {
		return findings
	}
	root := doc.Root.Content[0]
	if root.Kind != yaml.MappingNode {
		findings = append(findings, Finding{
			RuleID:   "DDINT001",
			Severity: SeverityError,
			Message:  "integration config must be a mapping",
			File:     path,
		})
		return findings
	}

	rootMap := map[string]*NodeWithPath{}
	collectMappingNodes(root, "", rootMap)

	checkNameNode := getNode(rootMap, "check_name")
	inferredName, inferredOk := inferCheckNameFromPath(path)
	if checkNameNode == nil || checkNameNode.Node.Value == "" {
		if inferredOk {
			checkNameNode = &NodeWithPath{Path: "check_name", Node: &yaml.Node{Kind: yaml.ScalarNode, Value: inferredName}}
		} else {
			pos := LookupPosition(doc.Positions, "check_name")
			findings = append(findings, Finding{
				RuleID:   "DDINT002",
				Severity: SeverityWarn,
				Message:  "missing check_name",
				File:     path,
				Path:     "check_name",
				Position: pos,
			})
		}
	} else if inferredOk && checkNameNode.Node.Value != inferredName {
		pos := LookupPosition(doc.Positions, "check_name")
		findings = append(findings, Finding{
			RuleID:   "DDINT015",
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("check_name %q does not match inferred integration name %q", checkNameNode.Node.Value, inferredName),
			File:     path,
			Path:     "check_name",
			Position: pos,
		})
	}

	instancesNode := lookupTopLevel(root, "instances")
	if instancesNode == nil {
		pos := LookupPosition(doc.Positions, "instances")
		findings = append(findings, Finding{
			RuleID:   "DDINT003",
			Severity: SeverityError,
			Message:  "missing instances list",
			File:     path,
			Path:     "instances",
			Position: pos,
		})
		return findings
	}
	if instancesNode.Kind != yaml.SequenceNode {
		pos := LookupPosition(doc.Positions, "instances")
		findings = append(findings, Finding{
			RuleID:   "DDINT004",
			Severity: SeverityError,
			Message:  "instances must be a list",
			File:     path,
			Path:     "instances",
			Position: pos,
		})
		return findings
	}

	initNode := lookupTopLevel(root, "init_config")
	if initNode != nil && initNode.Kind != yaml.MappingNode {
		pos := LookupPosition(doc.Positions, "init_config")
		findings = append(findings, Finding{
			RuleID:   "DDINT005",
			Severity: SeverityError,
			Message:  "init_config must be a map",
			File:     path,
			Path:     "init_config",
			Position: pos,
		})
	}

	checkName := ""
	if checkNameNode != nil {
		checkName = checkNameNode.Node.Value
	}
	integrationSchema, hasSchema := l.IntegrationSchemas[checkName]

	for idx, instance := range instancesNode.Content {
		if instance.Kind != yaml.MappingNode {
			findings = append(findings, Finding{
				RuleID:   "DDINT006",
				Severity: SeverityError,
				Message:  fmt.Sprintf("instances[%d] must be a map", idx),
				File:     path,
				Path:     fmt.Sprintf("instances[%d]", idx),
			})
			continue
		}
		findings = append(findings, lintCommonInstance(path, doc, idx, &NodeWithPath{Path: fmt.Sprintf("instances[%d]", idx), Node: instance})...)
		if hasSchema {
			findings = append(findings, lintInstanceSchema(path, doc, idx, instance, integrationSchema.Instances)...)
		}
		if checkName == "container_image" {
			findings = append(findings, lintContainerImageInstance(path, doc, idx, &NodeWithPath{Path: fmt.Sprintf("instances[%d]", idx), Node: instance})...)
		}
	}

	if hasSchema {
		findings = append(findings, lintInitSchema(path, doc, initNode, integrationSchema.InitConfig)...)
	}

	findings = append(findings, findNonFiniteFloats(path, doc, root, nil)...)

	return findings
}

func inferCheckNameFromPath(path string) (string, bool) {
	slashPath := filepath.ToSlash(path)
	segments := strings.Split(slashPath, "/")
	for i, segment := range segments {
		if segment == "conf.d" || segment == "confd" {
			if i+1 < len(segments) {
				dir := segments[i+1]
				if strings.HasSuffix(dir, ".d") {
					name := strings.TrimSuffix(dir, ".d")
					if name != "" {
						return name, true
					}
				}
			}
			return "", false
		}
	}
	return "", false
}

func lintCommonInstance(path string, doc YamlDocument, idx int, instance *NodeWithPath) []Finding {
	findings := []Finding{}
	instanceMap := map[string]*NodeWithPath{}
	collectMappingNodes(instance.Node, fmt.Sprintf("instances[%d]", idx), instanceMap)

	if node := getNode(instanceMap, fmt.Sprintf("instances[%d].min_collection_interval", idx)); node != nil {
		if node.Node.Tag == "!!int" || node.Node.Tag == "!!float" {
			if node.Node.Value == "0" || strings.HasPrefix(node.Node.Value, "-") {
				pos := LookupPosition(doc.Positions, fmt.Sprintf("instances[%d].min_collection_interval", idx))
				findings = append(findings, Finding{
					RuleID:   "DDINT007",
					Severity: SeverityWarn,
					Message:  "min_collection_interval should be > 0",
					File:     path,
					Path:     fmt.Sprintf("instances[%d].min_collection_interval", idx),
					Position: pos,
				})
			}
		}
	}

	if node := getNode(instanceMap, fmt.Sprintf("instances[%d].tags", idx)); node != nil {
		if node.Node.Kind != yaml.SequenceNode {
			pos := LookupPosition(doc.Positions, fmt.Sprintf("instances[%d].tags", idx))
			findings = append(findings, Finding{
				RuleID:   "DDINT008",
				Severity: SeverityError,
				Message:  "tags must be a list of strings",
				File:     path,
				Path:     fmt.Sprintf("instances[%d].tags", idx),
				Position: pos,
			})
		} else {
			for _, tagNode := range node.Node.Content {
				if tagNode.Tag != "!!str" {
					pos := LookupPosition(doc.Positions, fmt.Sprintf("instances[%d].tags", idx))
					findings = append(findings, Finding{
						RuleID:   "DDINT008",
						Severity: SeverityError,
						Message:  "tags must be a list of strings",
						File:     path,
						Path:     fmt.Sprintf("instances[%d].tags", idx),
						Position: pos,
					})
					break
				}
			}
		}
	}

	if node := getNode(instanceMap, fmt.Sprintf("instances[%d].service", idx)); node != nil {
		if node.Node.Tag != "!!str" {
			pos := LookupPosition(doc.Positions, fmt.Sprintf("instances[%d].service", idx))
			findings = append(findings, Finding{
				RuleID:   "DDINT009",
				Severity: SeverityError,
				Message:  "service must be a string",
				File:     path,
				Path:     fmt.Sprintf("instances[%d].service", idx),
				Position: pos,
			})
		}
	}

	return findings
}

func lintContainerImageInstance(path string, doc YamlDocument, idx int, instance *NodeWithPath) []Finding {
	findings := []Finding{}
	instanceMap := map[string]*NodeWithPath{}
	collectMappingNodes(instance.Node, fmt.Sprintf("instances[%d]", idx), instanceMap)

	if node := getNode(instanceMap, fmt.Sprintf("instances[%d].chunk_size", idx)); node != nil {
		if isIntOutOfRange(node, 1, 100) {
			pos := LookupPosition(doc.Positions, fmt.Sprintf("instances[%d].chunk_size", idx))
			findings = append(findings, Finding{
				RuleID:   "DDINT010",
				Severity: SeverityWarn,
				Message:  "chunk_size should be between 1 and 100",
				File:     path,
				Path:     fmt.Sprintf("instances[%d].chunk_size", idx),
				Position: pos,
			})
		}
	}

	if node := getNode(instanceMap, fmt.Sprintf("instances[%d].periodic_refresh_seconds", idx)); node != nil {
		if isIntOutOfRange(node, 60, 86400) {
			pos := LookupPosition(doc.Positions, fmt.Sprintf("instances[%d].periodic_refresh_seconds", idx))
			findings = append(findings, Finding{
				RuleID:   "DDINT011",
				Severity: SeverityWarn,
				Message:  "periodic_refresh_seconds should be between 60 and 86400",
				File:     path,
				Path:     fmt.Sprintf("instances[%d].periodic_refresh_seconds", idx),
				Position: pos,
			})
		}
	}

	return findings
}

func isIntOutOfRange(node *NodeWithPath, min int, max int) bool {
	if node.Node.Tag != "!!int" {
		return false
	}
	value := strings.TrimSpace(node.Node.Value)
	if value == "" {
		return false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return false
	}
	return parsed < min || parsed > max
}

type NodeWithPath struct {
	Path string
	Node *yaml.Node
}

func collectMappingNodes(node *yaml.Node, prefix string, out map[string]*NodeWithPath) {
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		path := keyNode.Value
		if prefix != "" {
			path = prefix + "." + keyNode.Value
		}
		out[path] = &NodeWithPath{Path: path, Node: valueNode}
		if valueNode.Kind == yaml.MappingNode {
			collectMappingNodes(valueNode, path, out)
		}
	}
}

func getNode(nodes map[string]*NodeWithPath, key string) *NodeWithPath {
	if node, ok := nodes[key]; ok {
		return node
	}
	return nil
}

func lookupTopLevel(root *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(root.Content); i += 2 {
		k := root.Content[i]
		v := root.Content[i+1]
		if k.Value == key {
			return v
		}
	}
	return nil
}

func schemaPrefixes(schema Schema) map[string]struct{} {
	prefixes := map[string]struct{}{}
	for key := range schema.Entries {
		parts := strings.Split(key, ".")
		for i := 1; i < len(parts); i++ {
			prefixes[strings.Join(parts[:i], ".")] = struct{}{}
		}
	}
	return prefixes
}

func schemaKeyKnown(schema Schema, prefixes map[string]struct{}, key string) bool {
	if _, ok := schema.Entries[key]; ok {
		return true
	}
	_, ok := prefixes[key]
	return ok
}

func schemaAllowsNested(schema Schema, key string) bool {
	parts := strings.Split(key, ".")
	for i := len(parts) - 1; i > 0; i-- {
		prefix := strings.Join(parts[:i], ".")
		entry, ok := schema.Entries[prefix]
		if !ok {
			continue
		}
		if entry.ValueType == schemaObject || entry.ValueType == schemaArray {
			return true
		}
	}
	return false
}

func lintInitSchema(path string, doc YamlDocument, initNode *yaml.Node, schema Schema) []Finding {
	if initNode == nil || initNode.Kind != yaml.MappingNode {
		return nil
	}
	return lintNodeAgainstSchema(path, doc, initNode, "init_config", schema)
}

func lintInstanceSchema(path string, doc YamlDocument, idx int, instance *yaml.Node, schema Schema) []Finding {
	return lintNodeAgainstSchema(path, doc, instance, fmt.Sprintf("instances[%d]", idx), schema)
}

func lintNodeAgainstSchema(path string, doc YamlDocument, node *yaml.Node, prefix string, schema Schema) []Finding {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	findings := []Finding{}
	nodes := map[string]*NodeWithPath{}
	collectMappingNodes(node, prefix, nodes)
	prefixes := schemaPrefixes(schema)
	for key, entryNode := range nodes {
		relativeKey := strings.TrimPrefix(key, prefix+".")
		if !schemaKeyKnown(schema, prefixes, relativeKey) {
			if schemaAllowsNested(schema, relativeKey) {
				continue
			}
			pos := LookupPosition(doc.Positions, key)
			findings = append(findings, Finding{
				RuleID:   "DDINT013",
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("unknown configuration key: %s", relativeKey),
				File:     path,
				Path:     key,
				Position: pos,
			})
			continue
		}
		schemaEntry, ok := schema.Entries[relativeKey]
		if !ok || schemaEntry.ValueType == schemaUnknown {
			continue
		}
		nodeType := YAMLNodeType(entryNode.Node)
		if nodeType != schemaEntry.ValueType {
			pos := LookupPosition(doc.Positions, key)
			findings = append(findings, Finding{
				RuleID:   "DDINT014",
				Severity: SeverityError,
				Message:  fmt.Sprintf("type mismatch for %s: expected %s, got %s", relativeKey, schemaEntry.ValueType, nodeType),
				File:     path,
				Path:     key,
				Position: pos,
			})
		}
	}
	return findings
}

func findNonFiniteFloats(path string, doc YamlDocument, node *yaml.Node, current []string) []Finding {
	findings := []Finding{}
	if node == nil {
		return findings
	}
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]
			next := append(current, keyNode.Value)
			findings = append(findings, findNonFiniteFloats(path, doc, valueNode, next)...)
		}
	case yaml.SequenceNode:
		for idx, child := range node.Content {
			next := append(current, fmt.Sprintf("[%d]", idx))
			findings = append(findings, findNonFiniteFloats(path, doc, child, next)...)
		}
	case yaml.ScalarNode:
		if node.Tag != "!!float" {
			return findings
		}
		value := strings.ToLower(strings.TrimSpace(node.Value))
		if value == ".inf" || value == "+.inf" || value == "-.inf" || value == ".nan" {
			pathKey := strings.Join(current, ".")
			pos := LookupPosition(doc.Positions, pathKey)
			findings = append(findings, Finding{
				RuleID:   "DDINT012",
				Severity: SeverityError,
				Message:  fmt.Sprintf("non-finite float value %q is not supported", node.Value),
				File:     path,
				Path:     pathKey,
				Position: pos,
			})
		}
	}
	return findings
}

