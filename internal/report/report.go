package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// Finding is the common representation emitted by JSHound's analyzers.
type Finding struct {
	Kind       string `json:"kind"`
	Value      string `json:"value"`
	Source     string `json:"source,omitempty"`
	Line       int    `json:"line,omitempty"`
	Rule       string `json:"rule,omitempty"`
	Status     string `json:"status,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Live       *bool  `json:"live,omitempty"`
}

// Summary contains totals by finding kind.
type Summary map[string]int

// Result is the stable machine-readable output contract.
type Result struct {
	Version  string    `json:"version"`
	Findings []Finding `json:"findings"`
	Summary  Summary   `json:"summary"`
}

// NewResult sorts and summarizes findings for deterministic output.
func NewResult(version string, findings []Finding) Result {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		if findings[i].Value != findings[j].Value {
			return findings[i].Value < findings[j].Value
		}
		return findings[i].Source < findings[j].Source
	})
	summary := make(Summary)
	for _, finding := range findings {
		summary[finding.Kind]++
	}
	return Result{Version: version, Findings: findings, Summary: summary}
}

func WriteJSON(w io.Writer, result Result) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(result)
}

func WriteJSONL(w io.Writer, result Result) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	for _, finding := range result.Findings {
		if err := encoder.Encode(finding); err != nil {
			return err
		}
	}
	return nil
}

func WriteText(w io.Writer, result Result) error {
	for _, finding := range result.Findings {
		meta := ""
		if finding.Status != "" {
			meta = " [" + finding.Status + "]"
		}
		if finding.Source != "" {
			meta += " (" + finding.Source + ")"
		}
		if _, err := fmt.Fprintf(w, "[%s] %s%s\n", finding.Kind, finding.Value, meta); err != nil {
			return err
		}
	}
	return nil
}
