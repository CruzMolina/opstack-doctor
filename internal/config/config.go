package config

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Chain      ChainConfig      `yaml:"chain" json:"chain"`
	Execution  ExecutionConfig  `yaml:"execution" json:"execution"`
	OPNodes    []OPNodeConfig   `yaml:"op_nodes" json:"op_nodes"`
	Proxyd     ProxydConfig     `yaml:"proxyd" json:"proxyd"`
	Interop    InteropConfig    `yaml:"interop" json:"interop"`
	Thresholds ThresholdsConfig `yaml:"thresholds" json:"thresholds"`
}

type ChainConfig struct {
	Name    string `yaml:"name" json:"name"`
	ChainID uint64 `yaml:"chain_id" json:"chain_id"`
}

type ExecutionConfig struct {
	ReferenceRPC     string `yaml:"reference_rpc" json:"reference_rpc"`
	CandidateRPC     string `yaml:"candidate_rpc" json:"candidate_rpc"`
	CompareBlocks    int    `yaml:"compare_blocks" json:"compare_blocks"`
	MaxHeadLagBlocks uint64 `yaml:"max_head_lag_blocks" json:"max_head_lag_blocks"`
}

type OPNodeConfig struct {
	Name    string `yaml:"name" json:"name"`
	Role    string `yaml:"role" json:"role"`
	RPC     string `yaml:"rpc" json:"rpc"`
	Metrics string `yaml:"metrics" json:"metrics"`
	Follows string `yaml:"follows" json:"follows"`
}

type ProxydConfig struct {
	Enabled   bool                   `yaml:"enabled" json:"enabled"`
	Endpoints []ProxydEndpointConfig `yaml:"endpoints" json:"endpoints"`
}

type ProxydEndpointConfig struct {
	Name             string   `yaml:"name" json:"name"`
	Role             string   `yaml:"role" json:"role"`
	RPC              string   `yaml:"rpc" json:"rpc"`
	Metrics          string   `yaml:"metrics" json:"metrics"`
	ConsensusAware   bool     `yaml:"consensus_aware" json:"consensus_aware"`
	ExpectedBackends []string `yaml:"expected_backends" json:"expected_backends"`
}

type InteropConfig struct {
	Enabled      bool               `yaml:"enabled" json:"enabled"`
	Dependencies []DependencyConfig `yaml:"dependencies" json:"dependencies"`
}

type DependencyConfig struct {
	Name    string `yaml:"name" json:"name"`
	ChainID uint64 `yaml:"chain_id" json:"chain_id"`
	RPC     string `yaml:"rpc" json:"rpc"`
	Metrics string `yaml:"metrics" json:"metrics"`
}

type ThresholdsConfig struct {
	MaxSafeLagBlocks     uint64  `yaml:"max_safe_lag_blocks" json:"max_safe_lag_blocks"`
	MinPeerCount         float64 `yaml:"min_peer_count" json:"min_peer_count"`
	MaxRPCLatencySeconds float64 `yaml:"max_rpc_latency_seconds" json:"max_rpc_latency_seconds"`
}

type ValidationIssue struct {
	Severity string
	Field    string
	Message  string
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	return Parse(f)
}

func Parse(r io.Reader) (Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(r)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, err
	}
	cfg.ApplyDefaults()
	return cfg, nil
}

func (c *Config) ApplyDefaults() {
	if c.Execution.CompareBlocks == 0 {
		c.Execution.CompareBlocks = 16
	}
	if c.Execution.MaxHeadLagBlocks == 0 {
		c.Execution.MaxHeadLagBlocks = 4
	}
	if c.Thresholds.MaxSafeLagBlocks == 0 {
		c.Thresholds.MaxSafeLagBlocks = 20
	}
	if c.Thresholds.MinPeerCount == 0 {
		c.Thresholds.MinPeerCount = 1
	}
	if c.Thresholds.MaxRPCLatencySeconds == 0 {
		c.Thresholds.MaxRPCLatencySeconds = 2
	}
}

func (c Config) Validate() []ValidationIssue {
	var issues []ValidationIssue
	add := func(severity, field, message string) {
		issues = append(issues, ValidationIssue{Severity: severity, Field: field, Message: message})
	}

	if strings.TrimSpace(c.Chain.Name) == "" {
		add("fail", "chain.name", "chain name is required")
	}
	if c.Chain.ChainID == 0 {
		add("fail", "chain.chain_id", "chain_id is required")
	}
	validateURLField(&issues, "execution.reference_rpc", c.Execution.ReferenceRPC, true)
	validateURLField(&issues, "execution.candidate_rpc", c.Execution.CandidateRPC, true)
	if c.Execution.CompareBlocks <= 0 {
		add("fail", "execution.compare_blocks", "compare_blocks must be greater than zero")
	}

	names := map[string]OPNodeConfig{}
	sourceNames := map[string]struct{}{}
	for i, node := range c.OPNodes {
		prefix := fmt.Sprintf("op_nodes[%d]", i)
		if strings.TrimSpace(node.Name) == "" {
			add("fail", prefix+".name", "op-node name is required")
		}
		if _, ok := validRoles()[node.Role]; !ok {
			add("fail", prefix+".role", "role must be one of source, light, sequencer, standalone")
		}
		if node.Name != "" {
			if _, exists := names[node.Name]; exists {
				add("fail", prefix+".name", "op-node names must be unique")
			}
			names[node.Name] = node
		}
		if node.Role == "source" && node.Name != "" {
			sourceNames[node.Name] = struct{}{}
		}
		validateURLField(&issues, prefix+".rpc", node.RPC, false)
		validateURLField(&issues, prefix+".metrics", node.Metrics, false)
	}
	if len(c.OPNodes) > 0 {
		switch len(sourceNames) {
		case 0:
			add("warn", "op_nodes", "no source op-node is configured")
		case 1:
			add("warn", "op_nodes", "only one source op-node is configured; source-tier redundancy is recommended")
		}
	}
	for i, node := range c.OPNodes {
		if node.Role != "light" && node.Role != "sequencer" {
			continue
		}
		field := fmt.Sprintf("op_nodes[%d].follows", i)
		if strings.TrimSpace(node.Follows) == "" {
			add("warn", field, "light and sequencer op-nodes should declare the source node they follow")
			continue
		}
		target, ok := names[node.Follows]
		if !ok {
			add("fail", field, "follows must point to a configured source op-node")
			continue
		}
		if target.Role != "source" {
			add("fail", field, "follows must point to an op-node with role source")
		}
	}

	if c.Proxyd.Enabled {
		if len(c.Proxyd.Endpoints) == 0 {
			add("warn", "proxyd.endpoints", "proxyd checks are enabled but no proxyd endpoints are configured")
		}
		proxydNames := map[string]struct{}{}
		for i, endpoint := range c.Proxyd.Endpoints {
			prefix := fmt.Sprintf("proxyd.endpoints[%d]", i)
			if strings.TrimSpace(endpoint.Name) == "" {
				add("fail", prefix+".name", "proxyd endpoint name is required")
			}
			if endpoint.Name != "" {
				if _, exists := proxydNames[endpoint.Name]; exists {
					add("fail", prefix+".name", "proxyd endpoint names must be unique")
				}
				proxydNames[endpoint.Name] = struct{}{}
			}
			if _, ok := validProxydRoles()[endpoint.Role]; !ok {
				add("fail", prefix+".role", "role must be one of deriver, edge, general")
			}
			validateURLField(&issues, prefix+".rpc", endpoint.RPC, false)
			validateURLField(&issues, prefix+".metrics", endpoint.Metrics, false)
			if endpoint.Role == "deriver" && !endpoint.ConsensusAware {
				add("warn", prefix+".consensus_aware", "deriver-tier proxyd should be configured as consensus aware")
			}
			if endpoint.Role == "deriver" && len(endpoint.ExpectedBackends) < 2 {
				add("warn", prefix+".expected_backends", "deriver-tier proxyd should front redundant source nodes")
			}
			for j, backendName := range endpoint.ExpectedBackends {
				field := fmt.Sprintf("%s.expected_backends[%d]", prefix, j)
				backend, ok := names[backendName]
				if !ok {
					add("fail", field, "expected backend must point to a configured op-node")
					continue
				}
				switch endpoint.Role {
				case "deriver":
					if backend.Role != "source" {
						add("fail", field, "deriver-tier proxyd backends must be source op-nodes")
					}
				case "edge":
					if backend.Role == "source" {
						add("warn", field, "edge proxyd usually fronts the light-node tier, not source nodes")
					}
				}
			}
		}
	}

	if c.Interop.Enabled {
		for i, dep := range c.Interop.Dependencies {
			prefix := fmt.Sprintf("interop.dependencies[%d]", i)
			if strings.TrimSpace(dep.Name) == "" {
				add("fail", prefix+".name", "dependency name is required")
			}
			if dep.ChainID == 0 {
				add("fail", prefix+".chain_id", "dependency chain_id is required")
			}
			validateURLField(&issues, prefix+".rpc", dep.RPC, true)
			validateURLField(&issues, prefix+".metrics", dep.Metrics, false)
		}
	}
	return issues
}

func validRoles() map[string]struct{} {
	return map[string]struct{}{
		"source":     {},
		"light":      {},
		"sequencer":  {},
		"standalone": {},
	}
}

func validProxydRoles() map[string]struct{} {
	return map[string]struct{}{
		"deriver": {},
		"edge":    {},
		"general": {},
	}
}

func validateURLField(issues *[]ValidationIssue, field, raw string, required bool) {
	if strings.TrimSpace(raw) == "" {
		if required {
			*issues = append(*issues, ValidationIssue{Severity: "fail", Field: field, Message: "URL is required"})
		}
		return
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		*issues = append(*issues, ValidationIssue{Severity: "fail", Field: field, Message: "URL must include scheme and host"})
		return
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		*issues = append(*issues, ValidationIssue{Severity: "fail", Field: field, Message: "URL scheme must be http or https"})
	}
}
