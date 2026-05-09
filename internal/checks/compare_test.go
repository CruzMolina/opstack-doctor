package checks

import (
	"testing"

	"opstack-doctor/internal/metrics"
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
