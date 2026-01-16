package lint

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Severity string

const (
	SeverityError Severity = "error"
	SeverityWarn  Severity = "warning"
	SeverityInfo  Severity = "info"
)

type Finding struct {
	RuleID   string   `json:"rule_id"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	File     string   `json:"file"`
	Path     string   `json:"path,omitempty"`
	Position Position `json:"position,omitempty"`
}

type Linter struct {
	Schema             Schema
	IntegrationSchemas map[string]IntegrationSchema
}

func NewLinter(schema Schema, integrationSchemas map[string]IntegrationSchema) *Linter {
	return &Linter{
		Schema:             schema,
		IntegrationSchemas: integrationSchemas,
	}
}

func (l *Linter) LintFile(path string, content []byte) ([]Finding, error) {
	var findings []Finding
	parsed, err := ParseYAMLWithPositions(content)
	if err != nil {
		findings = append(findings, Finding{
			RuleID:   "DDYAML001",
			Severity: SeverityError,
			Message:  fmt.Sprintf("invalid YAML: %v", err),
			File:     path,
		})
		return findings, nil
	}

	switch fileKind(path) {
	case "datadog":
		findings = append(findings, l.lintDatadog(path, parsed)...)
	case "integration":
		findings = append(findings, l.lintIntegration(path, parsed)...)
	default:
		return findings, nil
	}

	return findings, nil
}

func fileKind(path string) string {
	base := filepath.Base(path)
	slashPath := filepath.ToSlash(path)
	if base == "datadog.yaml" {
		return "datadog"
	}
	if base == "conf.yaml" && (strings.Contains(slashPath, "/conf.d/") || strings.Contains(slashPath, "/confd/")) {
		return "integration"
	}
	return "unknown"
}

