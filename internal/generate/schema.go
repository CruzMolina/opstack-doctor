package generate

import "encoding/json"

// Schema returns a JSON Schema for doctor.yaml. Keep this in sync with
// internal/config validation, but avoid encoding live-network assumptions here.
func Schema() ([]byte, error) {
	data, err := json.MarshalIndent(schemaDocument(), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func schemaDocument() map[string]any {
	httpURL := map[string]any{
		"type":        "string",
		"format":      "uri",
		"pattern":     "^https?://",
		"description": "HTTP(S) URL. URLs may contain credentials, but opstack-doctor redacts sensitive URL components before rendering findings.",
	}
	nonEmptyString := func(description string) map[string]any {
		return map[string]any{
			"type":        "string",
			"minLength":   1,
			"description": description,
		}
	}
	integerMinimum := func(min int, description string) map[string]any {
		return map[string]any{
			"type":        "integer",
			"minimum":     min,
			"description": description,
		}
	}
	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  "https://github.com/CruzMolina/opstack-doctor/schema/doctor.schema.json",
		"title":                "opstack-doctor configuration",
		"description":          "Read-only diagnostic configuration for OP Stack / Superchain operators. The config declares endpoint locations and intended topology; opstack-doctor validates live behavior through read-only RPC and metrics checks.",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"chain", "execution"},
		"properties": map[string]any{
			"chain": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"name", "chain_id"},
				"properties": map[string]any{
					"name":     nonEmptyString("Human-readable chain name used in reports and Prometheus labels."),
					"chain_id": integerMinimum(1, "Expected L2 chain ID. Compared against eth_chainId on execution endpoints."),
				},
			},
			"execution": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"reference_rpc", "candidate_rpc"},
				"properties": map[string]any{
					"reference_rpc": httpURL,
					"candidate_rpc": httpURL,
					"compare_blocks": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"default":     16,
						"description": "Number of latest common blocks to compare between reference and candidate execution clients.",
					},
					"max_head_lag_blocks": map[string]any{
						"type":        "integer",
						"minimum":     0,
						"default":     4,
						"description": "Maximum allowed candidate lag behind reference before a failure finding.",
					},
				},
			},
			"op_nodes": map[string]any{
				"type":        "array",
				"description": "Configured op-node fleet members and intended source/light/sequencer topology.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"name", "role"},
					"properties": map[string]any{
						"name":    nonEmptyString("Unique node name used in findings and follow-source references."),
						"role":    map[string]any{"type": "string", "enum": []string{"source", "light", "sequencer", "standalone"}, "description": "Intended op-node role."},
						"rpc":     httpURL,
						"metrics": httpURL,
						"follows": nonEmptyString("Name of the configured source op-node this light or sequencer node is intended to follow."),
					},
				},
			},
			"proxyd": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"description":          "Declared proxyd/routing endpoints and expected backend groups. opstack-doctor checks external RPC/metrics behavior, not private proxyd TOML.",
				"properties": map[string]any{
					"enabled": map[string]any{"type": "boolean", "default": false, "description": "Enables proxyd/routing topology checks."},
					"endpoints": map[string]any{
						"type":        "array",
						"description": "Declared proxyd endpoints to check.",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []string{"name", "role"},
							"properties": map[string]any{
								"name":              nonEmptyString("Unique proxyd endpoint name used in findings and alert labels."),
								"role":              map[string]any{"type": "string", "enum": []string{"deriver", "edge", "general"}, "description": "Intended proxyd role."},
								"rpc":               httpURL,
								"metrics":           httpURL,
								"consensus_aware":   map[string]any{"type": "boolean", "default": false, "description": "Declared operator intent that this proxyd endpoint uses consensus-aware routing."},
								"expected_backends": map[string]any{"type": "array", "items": nonEmptyString("Configured op-node name."), "description": "op-node names this proxyd endpoint is expected to route to or front."},
							},
						},
					},
				},
			},
			"interop": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"description":          "Basic dependency-set readiness checks. This schema and v0.x checker do not validate cross-chain messages or full interop protocol correctness.",
				"properties": map[string]any{
					"enabled": map[string]any{"type": "boolean", "default": false, "description": "Enables basic dependency endpoint checks."},
					"dependencies": map[string]any{
						"type":        "array",
						"description": "Chains in the configured dependency set.",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []string{"name", "chain_id", "rpc"},
							"properties": map[string]any{
								"name":     nonEmptyString("Human-readable dependency name."),
								"chain_id": integerMinimum(1, "Expected chain ID for the dependency."),
								"rpc":      httpURL,
								"metrics":  httpURL,
							},
						},
					},
					"supervisor": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"description":          "Optional op-supervisor metrics readiness checks.",
						"properties": map[string]any{
							"metrics":         httpURL,
							"expected_chains": map[string]any{"type": "array", "items": integerMinimum(1, "Expected dependency-set chain ID."), "description": "Chain IDs expected in op-supervisor refs."},
						},
					},
					"monitor": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"description":          "Optional op-interop-mon metrics readiness checks.",
						"properties": map[string]any{
							"metrics": httpURL,
						},
					},
				},
			},
			"thresholds": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"description":          "Operator thresholds used by warning/failure checks and generated alert templates.",
				"properties": map[string]any{
					"max_safe_lag_blocks":     map[string]any{"type": "integer", "minimum": 0, "default": 20, "description": "Warning threshold for follower safe-head or RPC-head lag behind source."},
					"min_peer_count":          map[string]any{"type": "number", "minimum": 0, "default": 1, "description": "Warning threshold for op-node peer count metrics."},
					"max_rpc_latency_seconds": map[string]any{"type": "number", "minimum": 0, "default": 2.0, "description": "Warning threshold for observed op-node/proxyd RPC latency metrics and generated latency alert templates."},
				},
			},
		},
	}
}
