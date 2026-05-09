package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

type Severity string

const (
	SeverityInfo Severity = "info"
	SeverityOK   Severity = "ok"
	SeverityWarn Severity = "warn"
	SeverityFail Severity = "fail"
)

type Finding struct {
	ID             string            `json:"id" yaml:"id"`
	Title          string            `json:"title" yaml:"title"`
	Severity       Severity          `json:"severity" yaml:"severity"`
	Target         string            `json:"target" yaml:"target"`
	Observed       string            `json:"observed" yaml:"observed"`
	Recommendation string            `json:"recommendation" yaml:"recommendation"`
	Docs           []string          `json:"docs,omitempty" yaml:"docs,omitempty"`
	Evidence       map[string]string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
}

type Summary struct {
	Info  int `json:"info" yaml:"info"`
	OK    int `json:"ok" yaml:"ok"`
	Warn  int `json:"warn" yaml:"warn"`
	Fail  int `json:"fail" yaml:"fail"`
	Total int `json:"total" yaml:"total"`
}

type JSONReport struct {
	Summary  Summary   `json:"summary"`
	Findings []Finding `json:"findings"`
}

func Summarize(findings []Finding) Summary {
	var s Summary
	for _, f := range findings {
		switch f.Severity {
		case SeverityInfo:
			s.Info++
		case SeverityOK:
			s.OK++
		case SeverityWarn:
			s.Warn++
		case SeverityFail:
			s.Fail++
		}
		s.Total++
	}
	return s
}

func ExitCode(findings []Finding, failOn string) (int, error) {
	s := Summarize(findings)
	switch strings.ToLower(strings.TrimSpace(failOn)) {
	case "", "none":
		return 0, nil
	case "fail":
		if s.Fail > 0 {
			return 1, nil
		}
		return 0, nil
	case "warn":
		if s.Fail > 0 || s.Warn > 0 {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("invalid --fail-on value %q: expected fail or warn", failOn)
	}
}

func RenderJSON(w io.Writer, findings []Finding) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(JSONReport{
		Summary:  Summarize(findings),
		Findings: findings,
	})
}

func RenderHuman(w io.Writer, findings []Finding) error {
	s := Summarize(findings)
	if _, err := fmt.Fprintf(w, "opstack-doctor summary: ok=%d info=%d warn=%d fail=%d total=%d\n\n", s.OK, s.Info, s.Warn, s.Fail, s.Total); err != nil {
		return err
	}
	for _, f := range findings {
		if _, err := fmt.Fprintf(w, "[%s] %s\n", strings.ToUpper(string(f.Severity)), f.Title); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  id: %s\n  target: %s\n  observed: %s\n", f.ID, f.Target, f.Observed); err != nil {
			return err
		}
		if f.Recommendation != "" {
			if _, err := fmt.Fprintf(w, "  recommendation: %s\n", f.Recommendation); err != nil {
				return err
			}
		}
		if len(f.Evidence) > 0 {
			keys := make([]string, 0, len(f.Evidence))
			for k := range f.Evidence {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if _, err := fmt.Fprintln(w, "  evidence:"); err != nil {
				return err
			}
			for _, k := range keys {
				if _, err := fmt.Fprintf(w, "    %s: %s\n", k, f.Evidence[k]); err != nil {
					return err
				}
			}
		}
		if len(f.Docs) > 0 {
			if _, err := fmt.Fprintf(w, "  docs: %s\n", strings.Join(f.Docs, ", ")); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}
