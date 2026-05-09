package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"opstack-doctor/internal/checks"
	"opstack-doctor/internal/config"
	"opstack-doctor/internal/demo"
	"opstack-doctor/internal/generate"
	"opstack-doctor/internal/report"
)

var version = "0.1.2"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "opstack-doctor v%s\n", version)
		return 0
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "generate":
		return runGenerate(args[1:], stdout, stderr)
	case "export":
		return runExport(args[1:], stdout, stderr)
	case "demo":
		return runDemo(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

func runDemo(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	scenario := fs.String("scenario", demo.ScenarioHealthy, "demo scenario: healthy, warn, or fail")
	output := fs.String("output", "human", "output format: human, json, or prometheus")
	failOn := fs.String("fail-on", "", "exit nonzero on severity: fail or warn")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, cleanup, err := demo.Config(*scenario)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	findings := checks.Runner{Timeout: 10 * time.Second}.Run(ctx, cfg)
	switch *output {
	case "human":
		if err := report.RenderHuman(stdout, findings); err != nil {
			fmt.Fprintf(stderr, "render report: %v\n", err)
			return 1
		}
	case "json":
		if err := report.RenderJSON(stdout, findings); err != nil {
			fmt.Fprintf(stderr, "render report: %v\n", err)
			return 1
		}
	case "prometheus":
		if err := report.RenderPrometheus(stdout, findings, report.PrometheusOptions{Chain: cfg.Chain.Name}); err != nil {
			fmt.Fprintf(stderr, "render prometheus report: %v\n", err)
			return 1
		}
	default:
		fmt.Fprintf(stderr, "invalid --output value %q: expected human, json, or prometheus\n", *output)
		return 2
	}
	code, err := report.ExitCode(findings, *failOn)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return code
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "doctor.yaml", "path to doctor YAML config")
	output := fs.String("output", "human", "output format: human, json, or prometheus")
	failOn := fs.String("fail-on", "", "exit nonzero on severity: fail or warn")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	findings := checks.Runner{Timeout: 10 * time.Second}.Run(ctx, cfg)
	switch *output {
	case "human":
		if err := report.RenderHuman(stdout, findings); err != nil {
			fmt.Fprintf(stderr, "render report: %v\n", err)
			return 1
		}
	case "json":
		if err := report.RenderJSON(stdout, findings); err != nil {
			fmt.Fprintf(stderr, "render report: %v\n", err)
			return 1
		}
	case "prometheus":
		if err := report.RenderPrometheus(stdout, findings, report.PrometheusOptions{Chain: cfg.Chain.Name}); err != nil {
			fmt.Fprintf(stderr, "render prometheus report: %v\n", err)
			return 1
		}
	default:
		fmt.Fprintf(stderr, "invalid --output value %q: expected human, json, or prometheus\n", *output)
		return 2
	}
	code, err := report.ExitCode(findings, *failOn)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return code
}

func runExport(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing export subcommand: metrics")
		return 2
	}
	switch args[0] {
	case "metrics":
		return runExportMetrics(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown export subcommand %q\n", args[0])
		return 2
	}
}

func runExportMetrics(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("export metrics", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "doctor.yaml", "path to doctor YAML config")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	findings := checks.Runner{Timeout: 10 * time.Second}.Run(ctx, cfg)
	if err := report.RenderPrometheus(stdout, findings, report.PrometheusOptions{Chain: cfg.Chain.Name}); err != nil {
		fmt.Fprintf(stderr, "render prometheus metrics: %v\n", err)
		return 1
	}
	return 0
}

func runGenerate(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing generate subcommand: alerts or runbook")
		return 2
	}
	switch args[0] {
	case "alerts":
		return runGenerateAlerts(args[1:], stdout, stderr)
	case "runbook":
		return runGenerateRunbook(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown generate subcommand %q\n", args[0])
		return 2
	}
}

func runGenerateAlerts(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("generate alerts", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "doctor.yaml", "path to doctor YAML config")
	outPath := fs.String("out", "", "path for generated Prometheus rules YAML")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *outPath == "" {
		fmt.Fprintln(stderr, "--out is required")
		return 2
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	data, err := generate.Alerts(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "generate alerts: %v\n", err)
		return 1
	}
	if err := os.WriteFile(*outPath, data, 0o644); err != nil {
		fmt.Fprintf(stderr, "write %s: %v\n", *outPath, err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\n", *outPath)
	return 0
}

func runGenerateRunbook(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("generate runbook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "doctor.yaml", "path to doctor YAML config")
	outPath := fs.String("out", "", "path for generated Markdown runbook")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *outPath == "" {
		fmt.Fprintln(stderr, "--out is required")
		return 2
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	if err := os.WriteFile(*outPath, generate.Runbook(cfg), 0o644); err != nil {
		fmt.Fprintf(stderr, "write %s: %v\n", *outPath, err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\n", *outPath)
	return 0
}

func usage(w io.Writer) {
	fmt.Fprint(w, `opstack-doctor is a read-only OP Stack diagnostic CLI.

Usage:
  opstack-doctor check --config doctor.yaml [--output human|json|prometheus] [--fail-on fail|warn]
  opstack-doctor check --config doctor.yaml --output prometheus
  opstack-doctor export metrics --config doctor.yaml
  opstack-doctor demo --scenario healthy|warn|fail [--output human|json|prometheus]
  opstack-doctor generate alerts --config doctor.yaml --out prometheus-rules.yaml
  opstack-doctor generate runbook --config doctor.yaml --out runbook.md
  opstack-doctor version
`)
}
