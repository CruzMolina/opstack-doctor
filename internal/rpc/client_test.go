package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientMethods(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		var result any
		switch req.Method {
		case "web3_clientVersion":
			result = "op-reth/v1.0.0"
		case "eth_chainId":
			result = "0xa"
		case "eth_blockNumber":
			result = "0x2a"
		case "eth_getBlockByNumber":
			result = map[string]any{
				"number":           req.Params[0],
				"hash":             "0xhash",
				"parentHash":       "0xparent",
				"stateRoot":        "0xstate",
				"transactionsRoot": "0xtx",
				"receiptsRoot":     "0xreceipt",
			}
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, time.Second)
	version, err := client.ClientVersion(context.Background())
	if err != nil || version != "op-reth/v1.0.0" {
		t.Fatalf("ClientVersion() = %q, %v", version, err)
	}
	chainID, err := client.ChainID(context.Background())
	if err != nil || chainID != 10 {
		t.Fatalf("ChainID() = %d, %v", chainID, err)
	}
	head, err := client.BlockNumber(context.Background())
	if err != nil || head != 42 {
		t.Fatalf("BlockNumber() = %d, %v", head, err)
	}
	block, err := client.BlockByNumber(context.Background(), 42)
	if err != nil {
		t.Fatalf("BlockByNumber() error = %v", err)
	}
	if got := StringValue(block.Hash); got != "0xhash" {
		t.Fatalf("block hash = %q", got)
	}
}

func TestParseQuantity(t *testing.T) {
	tests := map[string]uint64{
		"0x0": 0,
		"0xa": 10,
		"42":  42,
	}
	for in, want := range tests {
		got, err := ParseQuantity(in)
		if err != nil {
			t.Fatalf("ParseQuantity(%q) error = %v", in, err)
		}
		if got != want {
			t.Fatalf("ParseQuantity(%q) = %d, want %d", in, got, want)
		}
	}
}
