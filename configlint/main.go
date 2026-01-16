package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DataDog/datadog-agent/tools/configlint/lint"
)

func main() {
	var repoRoot string
	var integrationsCore string
	var integrationsExtras string
	flag.StringVar(&repoRoot, "repo-root", ".", "path to datadog-agent repo root")
	flag.StringVar(&integrationsCore, "integrations-core", "", "path to integrations-core repo (optional)")
	flag.StringVar(&integrationsExtras, "integrations-extras", "", "path to integrations-extras repo (optional)")
	flag.Parse()

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ddconfiglint [--repo-root PATH] <config files>")
		os.Exit(2)
	}

	schema, err := lint.LoadDatadogSchema(repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load schema: %v\n", err)
		os.Exit(2)
	}

	if integrationsCore == "" {
		integrationsCore = filepath.Join(filepath.Dir(repoRoot), "integrations-core")
	}
	if integrationsExtras == "" {
		integrationsExtras = filepath.Join(filepath.Dir(repoRoot), "integrations-extras")
	}

	integrationSchemas, err := lint.LoadIntegrationSchemas([]string{integrationsCore, integrationsExtras})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load integration schemas: %v\n", err)
		os.Exit(2)
	}

	linter := lint.NewLinter(schema, integrationSchemas)
	var findings []lint.Finding
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			findings = append(findings, lint.Finding{
				RuleID:   "DDYAML000",
				Severity: lint.SeverityError,
				Message:  fmt.Sprintf("failed to read file: %v", err),
				File:     file,
			})
			continue
		}
		fileFindings, err := linter.LintFile(filepath.Clean(file), content)
		if err != nil {
			findings = append(findings, lint.Finding{
				RuleID:   "DDYAML000",
				Severity: lint.SeverityError,
				Message:  fmt.Sprintf("lint failed: %v", err),
				File:     file,
			})
			continue
		}
		findings = append(findings, fileFindings...)
	}

	if err := lint.WriteJSON(os.Stdout, findings); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write report: %v\n", err)
		os.Exit(2)
	}
}

