package checks

import (
	"testing"

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
