package metrics

import (
	"strings"
	"testing"
)

func TestParseText(t *testing.T) {
	input := `
# HELP op_node_default_up up
op_node_default_up 1
op_node_default_refs_number{ref="l2_safe",source="source-1"} 123
op_node_default_rpc_client_request_duration_seconds_bucket{method="eth_getBlockByNumber",le="+Inf"} 7
escaped_label{path="a\"b\\c"} 2
`
	samples, err := ParseText(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseText() error = %v", err)
	}
	if len(samples) != 4 {
		t.Fatalf("len(samples) = %d, want 4", len(samples))
	}
	refs := Find(samples, "op_node_default_refs_number")
	if len(refs) != 1 {
		t.Fatalf("refs len = %d", len(refs))
	}
	if refs[0].Labels["ref"] != "l2_safe" || refs[0].Value != 123 {
		t.Fatalf("unexpected refs sample: %+v", refs[0])
	}
	latency := FindPrefix(samples, "op_node_default_rpc_client_request_duration_seconds")
	if len(latency) != 1 {
		t.Fatalf("latency prefix len = %d", len(latency))
	}
}
