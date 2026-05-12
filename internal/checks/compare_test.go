package checks

import (
	"testing"

	"opstack-doctor/internal/config"
	"opstack-doctor/internal/metrics"
	"opstack-doctor/internal/report"
	"opstack-doctor/internal/rpc"
)

func TestCompareBlockFields(t *testing.T) {
	ref := block("0xhash", "0xparent", "0xstate", "0xtx", "0xreceipt")
	cand := block("0xhash", "0xparent", "0xstate", "0xtx", "0xreceipt")
	diffs, missing := CompareBlockFields(ref, cand)
	if len(diffs) != 0 || len(missing) != 0 {
		t.Fatalf("CompareBlockFields() diffs=%+v missing=%+v", diffs, missing)
	}

	cand.StateRoot = ptr("0xdifferent")
	diffs, missing = CompareBlockFields(ref, cand)
	if len(diffs) != 1 || diffs[0].Field != "stateRoot" || len(missing) != 0 {
		t.Fatalf("CompareBlockFields() diffs=%+v missing=%+v", diffs, missing)
	}

	cand = block("0xhash", "0xparent", "0xstate", "0xtx", "")
	diffs, missing = CompareBlockFields(ref, cand)
	if len(diffs) != 0 || len(missing) != 1 || missing[0] != "receiptsRoot" {
		t.Fatalf("CompareBlockFields() missing receiptsRoot, got diffs=%+v missing=%+v", diffs, missing)
	}
}

func TestSafeRefDoesNotTreatUnsafeAsSafe(t *testing.T) {
	samples := []metrics.Sample{
		{Name: "op_node_default_refs_number", Labels: map[string]string{"ref": "l2_safe"}, Value: 100},
		{Name: "op_node_default_refs_number", Labels: map[string]string{"ref": "l2_unsafe"}, Value: 101},
	}
	got, ok := safeRef(samples)
	if !ok {
		t.Fatal("safeRef() ok = false")
	}
	if got != 100 {
		t.Fatalf("safeRef() = %.0f, want 100", got)
	}
}

func TestRefValueParsesL2LabelVariants(t *testing.T) {
	samples := []metrics.Sample{
		{Name: "op_node_default_refs_number", Labels: map[string]string{"ref": "l2_safe"}, Value: 100},
		{Name: "op_node_default_refs_number", Labels: map[string]string{"type": "safe", "layer": "L2"}, Value: 101},
		{Name: "op_node_default_refs_number", Labels: map[string]string{"name": "L2Safe"}, Value: 102},
		{Name: "op_node_default_refs_number", Labels: map[string]string{"ref_name": "L2-FINALIZED"}, Value: 90},
		{Name: "op_node_default_refs_number", Labels: map[string]string{"kind": "unsafe", "layer": "l2"}, Value: 105},
		{Name: "op_node_default_refs_number", Labels: map[string]string{"type": "unsafe_l2"}, Value: 106},
	}
	tests := []struct {
		name string
		want float64
	}{
		{name: "safe", want: 102},
		{name: "finalized", want: 90},
		{name: "unsafe", want: 106},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := refValue(samples, tt.name)
			if !ok {
				t.Fatalf("refValue(%q) ok = false", tt.name)
			}
			if got != tt.want {
				t.Fatalf("refValue(%q) = %.0f, want %.0f", tt.name, got, tt.want)
			}
		})
	}
}

func TestRefValueIgnoresAmbiguousAndL1Refs(t *testing.T) {
	samples := []metrics.Sample{
		{Name: "op_node_default_refs_number", Labels: map[string]string{"ref": "safe"}, Value: 200},
		{Name: "op_node_default_refs_number", Labels: map[string]string{"ref": "l1_safe"}, Value: 300},
		{Name: "op_node_default_refs_number", Labels: map[string]string{"status": "l2_safe"}, Value: 400},
	}
	if got, ok := safeRef(samples); ok {
		t.Fatalf("safeRef() = %.0f, want no parseable L2 safe ref", got)
	}
}

func TestTopologySafeHeadEvidenceIncludesMatchedSeries(t *testing.T) {
	findings := compareTopologyMetricSafeHeads(
		config.Config{Thresholds: config.ThresholdsConfig{MaxSafeLagBlocks: 20}},
		nodeMetricsState{
			node: config.OPNodeConfig{Name: "source-1"},
			samples: []metrics.Sample{
				{Name: "op_node_default_refs_number", Labels: map[string]string{"ref": "safe"}, Value: 500},
				{Name: "op_node_default_refs_number", Labels: map[string]string{"ref": "l2_safe"}, Value: 100},
			},
		},
		nodeMetricsState{
			node: config.OPNodeConfig{Name: "light-1"},
			samples: []metrics.Sample{
				{Name: "op_node_default_refs_number", Labels: map[string]string{"type": "safe", "layer": "l2"}, Value: 99},
			},
		},
	)
	if len(findings) != 1 {
		t.Fatalf("findings len = %d, want 1", len(findings))
	}
	finding := findings[0]
	if finding.Severity != report.SeverityOK {
		t.Fatalf("severity = %s, want ok", finding.Severity)
	}
	if finding.Evidence["source_safe"] != "100" || finding.Evidence["node_safe"] != "99" || finding.Evidence["lag_blocks"] != "1" {
		t.Fatalf("unexpected evidence: %+v", finding.Evidence)
	}
	if finding.Evidence["source_safe_series"] == "" || finding.Evidence["node_safe_series"] == "" {
		t.Fatalf("expected matched series evidence, got %+v", finding.Evidence)
	}
}

func block(hash, parent, state, tx, receipts string) rpc.Block {
	return rpc.Block{
		Hash:             optional(hash),
		ParentHash:       optional(parent),
		StateRoot:        optional(state),
		TransactionsRoot: optional(tx),
		ReceiptsRoot:     optional(receipts),
	}
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return ptr(s)
}

func ptr(s string) *string {
	return &s
}
