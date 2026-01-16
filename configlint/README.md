# ddconfiglint

`ddconfiglint` is a standalone linter for Datadog Agent configuration files. It validates:

- `datadog.yaml`
- `conf.d/<integration>.d/conf.yaml`

It is designed to match real Agent behavior by extracting schema information from the
Datadog Agent source tree and integration templates.

## Features

- YAML syntax validation with line/column reporting.
- Unknown key and type-mismatch detection for `datadog.yaml` and integrations.
- Deprecated key warnings based on `config_template.yaml` comments.
- Integration-specific schema checks based on `conf.yaml`/`conf.yaml.example` templates.
- Additional built-in rules (for example: non-finite floats, container_image ranges).

## Requirements

- Go 1.20+ (or a compatible version supported by your environment).
- A local clone of `datadog-agent`.
- Optional: local clones of `integrations-core` and `integrations-extras` next to the
  `datadog-agent` directory (recommended for deeper integration schema coverage).

## Build

From the repo root:

```bash
cd /path/to/datadog-agent/tools/configlint
GOWORK=off go build -o ddconfiglint .
```

If you see missing `go.sum` entries:

```bash
cd /path/to/datadog-agent/tools/configlint
GOWORK=off go mod tidy
GOWORK=off go build -o ddconfiglint .
```

## Usage

### Lint `datadog.yaml`

```bash
./ddconfiglint --repo-root /path/to/datadog-agent /path/to/datadog.yaml
```

### Lint an integration config

```bash
./ddconfiglint --repo-root /path/to/datadog-agent /path/to/conf.d/<integration>.d/conf.yaml
```

### Add a macOS alias (zsh)

If you want a convenient alias on macOS (zsh), add one to your `~/.zshrc`:

```bash
echo 'alias ddlint="<path_to_ddconfiglint> --repo-root <path_to_datadog_agent_repo>"' >> ~/.zshrc
source ~/.zshrc
example: echo 'alias ddlint="/Users/sunryeok.kim/dd/datadog-agent/tools/configlint/ddconfiglint --repo-root ~/dd/datadog-agent"' >> ~/.zshrc
```

### Integration schema sources

If the integrations repositories are siblings of `datadog-agent`:

```
/path/to/datadog-agent
/path/to/integrations-core
/path/to/integrations-extras
```

`ddconfiglint` will auto-discover them and load `conf.yaml`/`conf.yaml.example` schemas.

## Output

The tool emits a JSON report to stdout. Example:

```json
{
  "findings": [
    {
      "rule_id": "DDYAML003",
      "severity": "error",
      "message": "type mismatch for site: expected string, got number",
      "file": "/path/to/datadog.yaml",
      "path": "site",
      "position": {
        "line": 10,
        "column": 1
      }
    }
  ]
}
```

If no issues are found, `findings` is an empty array.

## Notes

- The linter does not modify files; it only reports findings.
- Integration schema coverage depends on the availability of template files in
  `integrations-core` and `integrations-extras`.

