package lint

import (
	"encoding/json"
	"io"
)

type Report struct {
	Findings []Finding `json:"findings"`
}

func WriteJSON(w io.Writer, findings []Finding) error {
	if findings == nil {
		findings = []Finding{}
	}
	report := Report{Findings: findings}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

