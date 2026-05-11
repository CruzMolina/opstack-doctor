package validate

import (
	"strings"

	"opstack-doctor/internal/config"
	"opstack-doctor/internal/report"
)

func Findings(cfg config.Config, target string) []report.Finding {
	if target == "" {
		target = "doctor.yaml"
	}
	cfg.ApplyDefaults()
	issues := cfg.Validate()
	if len(issues) == 0 {
		return []report.Finding{{
			ID:             "config.valid",
			Title:          "Configuration is valid",
			Severity:       report.SeverityOK,
			Target:         target,
			Observed:       "required fields and topology intent are valid",
			Recommendation: "Keep this file in version control and review it when topology changes.",
		}}
	}
	findings := make([]report.Finding, 0, len(issues))
	for _, issue := range issues {
		severity := report.SeverityWarn
		if issue.Severity == "fail" {
			severity = report.SeverityFail
		}
		findings = append(findings, report.Finding{
			ID:             "config." + strings.ReplaceAll(issue.Field, ".", "_"),
			Title:          "Configuration issue",
			Severity:       severity,
			Target:         issue.Field,
			Observed:       issue.Message,
			Recommendation: "Fix the configuration before relying on diagnostic results from affected checks.",
		})
	}
	return findings
}
