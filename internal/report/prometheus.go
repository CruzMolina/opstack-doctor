package report

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type PrometheusOptions struct {
	Chain string
}

func RenderPrometheus(w io.Writer, findings []Finding, opts PrometheusOptions) error {
	summary := Summarize(findings)
	if _, err := fmt.Fprintln(w, "# HELP opstack_doctor_findings Number of findings emitted by severity."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE opstack_doctor_findings gauge"); err != nil {
		return err
	}
	counts := map[string]int{
		"info": summary.Info,
		"ok":   summary.OK,
		"warn": summary.Warn,
		"fail": summary.Fail,
	}
	for _, severity := range []string{"ok", "info", "warn", "fail"} {
		if _, err := fmt.Fprintf(w, "opstack_doctor_findings{severity=%q} %d\n", severity, counts[severity]); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, "# HELP opstack_doctor_finding Finding presence by id, target, and severity. Value is always 1 for emitted findings."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE opstack_doctor_finding gauge"); err != nil {
		return err
	}
	for _, f := range findings {
		labels := map[string]string{
			"id":       f.ID,
			"severity": string(f.Severity),
			"target":   f.Target,
		}
		if opts.Chain != "" {
			labels["chain"] = opts.Chain
		}
		if _, err := fmt.Fprintf(w, "opstack_doctor_finding%s 1\n", renderLabels(labels)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	if err := renderNumericEvidence(w, findings, opts); err != nil {
		return err
	}
	return renderDerivedMetrics(w, findings, opts)
}

func renderNumericEvidence(w io.Writer, findings []Finding, opts PrometheusOptions) error {
	if _, err := fmt.Fprintln(w, "# HELP opstack_doctor_finding_evidence_value Numeric evidence values attached to findings."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE opstack_doctor_finding_evidence_value gauge"); err != nil {
		return err
	}
	for _, f := range findings {
		keys := make([]string, 0, len(f.Evidence))
		for key := range f.Evidence {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value, ok := parseEvidenceFloat(f.Evidence[key])
			if !ok {
				continue
			}
			labels := map[string]string{
				"id":       f.ID,
				"key":      key,
				"severity": string(f.Severity),
				"target":   f.Target,
			}
			if opts.Chain != "" {
				labels["chain"] = opts.Chain
			}
			if _, err := fmt.Fprintf(w, "opstack_doctor_finding_evidence_value%s %s\n", renderLabels(labels), formatPromFloat(value)); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	return nil
}

func renderDerivedMetrics(w io.Writer, findings []Finding, opts PrometheusOptions) error {
	if _, err := fmt.Fprintln(w, "# HELP opstack_doctor_execution_candidate_lag_blocks Candidate execution head lag behind the reference execution endpoint."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE opstack_doctor_execution_candidate_lag_blocks gauge"); err != nil {
		return err
	}
	if lag, ok := evidenceByFinding(findings, "execution.head_lag", "lag_blocks"); ok {
		if _, err := fmt.Fprintf(w, "opstack_doctor_execution_candidate_lag_blocks%s %s\n", renderLabels(chainLabels(opts)), formatPromFloat(lag)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, "# HELP opstack_doctor_execution_block_compare_match Whether the latest common execution block comparison matched. 1 means matched, 0 means divergence observed."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE opstack_doctor_execution_block_compare_match gauge"); err != nil {
		return err
	}
	if hasFinding(findings, "execution.block_compare.match") || hasFinding(findings, "execution.block_compare.divergence") {
		value := float64(0)
		if hasFinding(findings, "execution.block_compare.match") && !hasFailFinding(findings, "execution.block_compare.divergence") {
			value = 1
		}
		if _, err := fmt.Fprintf(w, "opstack_doctor_execution_block_compare_match%s %s\n", renderLabels(chainLabels(opts)), formatPromFloat(value)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, "# HELP opstack_doctor_topology_follower_lag_blocks Follower lag behind its configured source, derived from RPC head or safe-head metrics."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE opstack_doctor_topology_follower_lag_blocks gauge"); err != nil {
		return err
	}
	for _, f := range findings {
		if !strings.HasPrefix(f.ID, "topology.") {
			continue
		}
		kind := ""
		switch {
		case strings.HasSuffix(f.ID, ".rpc_head"):
			kind = "rpc_head"
		case strings.HasSuffix(f.ID, ".safe_head_metrics"):
			kind = "safe_head"
		default:
			continue
		}
		lag, ok := parseEvidenceFloat(f.Evidence["lag_blocks"])
		if !ok {
			continue
		}
		labels := chainLabels(opts)
		labels["kind"] = kind
		if node := topologyNodeFromID(f.ID); node != "" {
			labels["node"] = node
		}
		if source := f.Evidence["source"]; source != "" {
			labels["source"] = source
		}
		if _, err := fmt.Fprintf(w, "opstack_doctor_topology_follower_lag_blocks%s %s\n", renderLabels(labels), formatPromFloat(lag)); err != nil {
			return err
		}
	}
	return nil
}

func evidenceByFinding(findings []Finding, id, key string) (float64, bool) {
	for _, f := range findings {
		if f.ID != id {
			continue
		}
		return parseEvidenceFloat(f.Evidence[key])
	}
	return 0, false
}

func hasFinding(findings []Finding, id string) bool {
	for _, f := range findings {
		if f.ID == id {
			return true
		}
	}
	return false
}

func hasFailFinding(findings []Finding, id string) bool {
	for _, f := range findings {
		if f.ID == id && f.Severity == SeverityFail {
			return true
		}
	}
	return false
}

func topologyNodeFromID(id string) string {
	id = strings.TrimPrefix(id, "topology.")
	if idx := strings.IndexByte(id, '.'); idx >= 0 {
		return id[:idx]
	}
	return ""
}

func chainLabels(opts PrometheusOptions) map[string]string {
	labels := map[string]string{}
	if opts.Chain != "" {
		labels["chain"] = opts.Chain
	}
	return labels
}

func parseEvidenceFloat(raw string) (float64, bool) {
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func formatPromFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func renderLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		if labels[key] == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, sanitizeLabelName(key)+"="+strconv.Quote(labels[key]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

var invalidLabelNameChars = regexp.MustCompile(`[^a-zA-Z0-9_]`)

func sanitizeLabelName(name string) string {
	name = invalidLabelNameChars.ReplaceAllString(name, "_")
	if name == "" {
		return "label"
	}
	first := name[0]
	if (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_' {
		return name
	}
	return "_" + name
}
