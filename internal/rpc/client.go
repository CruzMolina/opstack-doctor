package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opstack-doctor/internal/redact"
)

type Client struct {
	endpoint string
	redacted string
	http     *http.Client
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e Error) Error() string {
	return fmt.Sprintf("json-rpc error %d: %s", e.Code, e.Message)
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *Error          `json:"error"`
}

type Block struct {
	Number           *string `json:"number"`
	Hash             *string `json:"hash"`
	ParentHash       *string `json:"parentHash"`
	StateRoot        *string `json:"stateRoot"`
	TransactionsRoot *string `json:"transactionsRoot"`
	ReceiptsRoot     *string `json:"receiptsRoot"`
}

func NewClient(endpoint string, timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		endpoint: endpoint,
		redacted: redact.URL(endpoint),
		http:     &http.Client{Timeout: timeout},
	}
}

func (c *Client) RedactedEndpoint() string {
	return c.redacted
}

func (c *Client) Call(ctx context.Context, method string, params []any, result any) error {
	body, err := json.Marshal(request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build rpc request for %s: %s", c.redacted, redact.String(err.Error(), c.endpoint))
	}
	req.Header.Set("content-type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("rpc request to %s failed: %s", c.redacted, redact.String(err.Error(), c.endpoint))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("rpc request to %s returned HTTP %d: %s", c.redacted, resp.StatusCode, redact.String(string(msg), c.endpoint))
	}
	var rpcResp response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return fmt.Errorf("decode rpc response from %s: %w", c.redacted, err)
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("rpc %s on %s failed: %w", method, c.redacted, *rpcResp.Error)
	}
	if result == nil {
		return nil
	}
	if len(rpcResp.Result) == 0 || string(rpcResp.Result) == "null" {
		return fmt.Errorf("rpc %s on %s returned empty result", method, c.redacted)
	}
	if err := json.Unmarshal(rpcResp.Result, result); err != nil {
		return fmt.Errorf("decode rpc %s result from %s: %w", method, c.redacted, err)
	}
	return nil
}

func (c *Client) ClientVersion(ctx context.Context) (string, error) {
	var out string
	err := c.Call(ctx, "web3_clientVersion", nil, &out)
	return out, err
}

func (c *Client) ChainID(ctx context.Context) (uint64, error) {
	var out string
	if err := c.Call(ctx, "eth_chainId", nil, &out); err != nil {
		return 0, err
	}
	return ParseQuantity(out)
}

func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {
	var out string
	if err := c.Call(ctx, "eth_blockNumber", nil, &out); err != nil {
		return 0, err
	}
	return ParseQuantity(out)
}

func (c *Client) BlockByNumber(ctx context.Context, number uint64) (Block, error) {
	var out Block
	err := c.Call(ctx, "eth_getBlockByNumber", []any{Quantity(number), false}, &out)
	return out, err
}

func ParseQuantity(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty quantity")
	}
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		if s == "0x" || s == "0X" {
			return 0, fmt.Errorf("empty hex quantity")
		}
		return strconv.ParseUint(s[2:], 16, 64)
	}
	return strconv.ParseUint(s, 10, 64)
}

func Quantity(n uint64) string {
	return fmt.Sprintf("0x%x", n)
}

func StringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (b Block) NumberUint64() (uint64, bool) {
	if b.Number == nil {
		return 0, false
	}
	n, err := ParseQuantity(*b.Number)
	return n, err == nil
}
