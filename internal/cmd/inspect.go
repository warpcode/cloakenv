package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
	"github.com/warpcode/cloakenv/internal/runner"
	"github.com/warpcode/cloakenv/internal/utils"
)

// InspectSourceReport captures secret-free entry match metadata and rule traces.
type InspectSourceReport struct {
	Vault      string             `json:"vault" yaml:"vault"`
	Path       string             `json:"path,omitempty" yaml:"path,omitempty"`
	Title      string             `json:"title,omitempty" yaml:"title,omitempty"`
	Tags       []string           `json:"tags,omitempty" yaml:"tags,omitempty"`
	OutputKeys []string           `json:"output_keys,omitempty" yaml:"output_keys,omitempty"`
	Traces     []engine.RuleTrace `json:"traces,omitempty" yaml:"traces,omitempty"`
}

// InspectEnvReport is the strictly redacted output of `cloakenv inspect env <name>`.
type InspectEnvReport struct {
	Kind    string                `json:"kind" yaml:"kind"`
	Name    string                `json:"name" yaml:"name"`
	Sources []InspectSourceReport `json:"sources" yaml:"sources"`
	Env     map[string]string     `json:"env" yaml:"env"`
}

// InspectTemplateReport is the strictly redacted output of `cloakenv inspect template <name>`.
type InspectTemplateReport struct {
	Kind         string                `json:"kind" yaml:"kind"`
	Name         string                `json:"name" yaml:"name"`
	Driver       string                `json:"driver" yaml:"driver"`
	ContentBytes int                   `json:"content_bytes" yaml:"content_bytes"`
	Sources      []InspectSourceReport `json:"sources" yaml:"sources"`
	Entries      []map[string]string   `json:"entries" yaml:"entries"`
}

// InspectTemplateDeliveryReport captures template delivery metadata without plaintext content.
type InspectTemplateDeliveryReport struct {
	Template  string `json:"template" yaml:"template"`
	Driver    string `json:"driver" yaml:"driver"`
	Mode      string `json:"mode" yaml:"mode"`
	Env       string `json:"env,omitempty" yaml:"env,omitempty"`
	EnvPath   string `json:"env_path,omitempty" yaml:"env_path,omitempty"`
	Path      string `json:"path,omitempty" yaml:"path,omitempty"`
	Ephemeral bool   `json:"ephemeral" yaml:"ephemeral"`
	Persist   bool   `json:"persist" yaml:"persist"`
}

// InspectRunReport is the strictly redacted output of `cloakenv inspect run -- <cmd>`.
type InspectRunReport struct {
	Kind      string                          `json:"kind" yaml:"kind"`
	Command   []string                        `json:"command" yaml:"command"`
	Env       map[string]string               `json:"env" yaml:"env"`
	Templates []InspectTemplateDeliveryReport `json:"templates" yaml:"templates"`
}

// InspectOverviewReport summarizes configured vaults, envs, templates, and autoload match patterns.
type InspectOverviewReport struct {
	Kind      string   `json:"kind" yaml:"kind"`
	Vaults    []string `json:"vaults" yaml:"vaults"`
	Envs      []string `json:"envs" yaml:"envs"`
	Templates []string `json:"templates" yaml:"templates"`
	Autoload  []string `json:"autoload" yaml:"autoload"`
}

// Inspect handles `cloakenv inspect [env <name> | template <name> | run -- <cmd>] [-o text|json|yaml]`.
// Every secret value is unconditionally masked as `<REDACTED>`; no flag may disable redaction.
func Inspect(args []string, cfg *config.Config, stdout, stderr io.Writer) int {
	var (
		outputFmt = "text"
		showHelp  bool
	)

	dashDashIdx := -1
	for i, a := range args {
		if a == "--" {
			dashDashIdx = i
			break
		}
	}

	var preArgs, postArgs []string
	if dashDashIdx >= 0 {
		preArgs = args[:dashDashIdx]
		postArgs = args[dashDashIdx+1:]
	} else {
		preArgs, postArgs = splitInspectRunArgs(args)
	}

	parser := NewFlagParser()
	parser.Bool([]string{"-h", "--help"}, &showHelp)
	parser.Var([]string{"-o", "--output"}, true, "flag -o/--output requires an argument", func(_, val string) error {
		v := strings.ToLower(strings.TrimSpace(val))
		if v != "text" && v != "json" && v != "yaml" {
			return fmt.Errorf("invalid output format %q (expected text, json, or yaml)", val)
		}
		outputFmt = v
		return nil
	})

	rem, err := parser.Parse(preArgs)
	if showHelp {
		PrintInspectHelpTo(stdout)
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if cfg == nil {
		cfg = &config.Config{}
	}

	if len(rem) == 0 && len(postArgs) == 0 {
		report := buildOverviewReport(cfg)
		if err := writeInspectOutput(stdout, outputFmt, report, formatOverviewText(report)); err != nil {
			fmt.Fprintf(stderr, "Inspect output failed: %v\n", err)
			return 1
		}
		return 0
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Config error: %v\n", err)
		return 1
	}
	defer func() { _ = orch.Close() }()

	ctx, stop := signalContext()
	defer stop()

	if len(rem) == 0 && len(postArgs) > 0 {
		return runInspectCommand(ctx, orch, postArgs, outputFmt, stdout, stderr)
	}

	sub := rem[0]
	switch sub {
	case "env":
		if len(rem) != 2 || len(postArgs) > 0 {
			fmt.Fprintln(stderr, "Usage: cloakenv inspect env <name> [-o text|json|yaml]")
			return 1
		}
		return runInspectEnv(ctx, orch, rem[1], outputFmt, stdout, stderr)

	case "template":
		if len(rem) != 2 || len(postArgs) > 0 {
			fmt.Fprintln(stderr, "Usage: cloakenv inspect template <name> [-o text|json|yaml]")
			return 1
		}
		return runInspectTemplate(ctx, orch, rem[1], outputFmt, stdout, stderr)

	case "run", "command":
		var cmdArgs []string
		if len(postArgs) > 0 {
			cmdArgs = append(cmdArgs, rem[1:]...)
			cmdArgs = append(cmdArgs, postArgs...)
		} else {
			cmdArgs = rem[1:]
		}
		if len(cmdArgs) == 0 {
			fmt.Fprintln(stderr, "Usage: cloakenv inspect run -- <command> [args]")
			return 1
		}
		return runInspectCommand(ctx, orch, cmdArgs, outputFmt, stdout, stderr)

	default:
		if len(rem) == 1 && len(postArgs) == 0 {
			_, hasEnv := cfg.Envs[sub]
			_, hasTpl := cfg.Templates[sub]
			switch {
			case hasEnv && hasTpl:
				fmt.Fprintf(stderr, "Ambiguous inspect target %q exists in both envs and templates; specify 'inspect env %s' or 'inspect template %s'\n", sub, sub, sub)
				return 1
			case hasEnv:
				return runInspectEnv(ctx, orch, sub, outputFmt, stdout, stderr)
			case hasTpl:
				return runInspectTemplate(ctx, orch, sub, outputFmt, stdout, stderr)
			}
		}
		fmt.Fprintf(stderr, "Unknown inspect target %q. Expected: env <name>, template <name>, or run -- <command>\n", sub)
		return 1
	}
}

func runInspectEnv(ctx context.Context, orch *engine.Orchestrator, name, format string, stdout, stderr io.Writer) int {
	res, err := orch.EnvsBuilder().BuildDetailed(ctx, name)
	if err != nil {
		fmt.Fprintf(stderr, "Inspect env failed: %v\n", err)
		return exitCodeForContext(ctx, 1)
	}

	redactedEnv := make(map[string]string, len(res.Env))
	for k := range res.Env {
		redactedEnv[k] = runner.RedactedPlaceholder
	}

	report := InspectEnvReport{
		Kind:    "env",
		Name:    name,
		Sources: toInspectSourceReports(res.Sources),
		Env:     redactedEnv,
	}

	if err := writeInspectOutput(stdout, format, report, formatEnvReportText(report)); err != nil {
		fmt.Fprintf(stderr, "Inspect output failed: %v\n", err)
		return 1
	}
	return 0
}

func runInspectTemplate(ctx context.Context, orch *engine.Orchestrator, name, format string, stdout, stderr io.Writer) int {
	res, err := orch.TemplatesRenderer().RenderDetailed(ctx, name)
	if err != nil {
		fmt.Fprintf(stderr, "Inspect template failed: %v\n", err)
		return exitCodeForContext(ctx, 1)
	}

	redactedEntries := make([]map[string]string, 0, len(res.Entries))
	for _, entry := range res.Entries {
		redacted := make(map[string]string, len(entry))
		for k := range entry {
			redacted[k] = runner.RedactedPlaceholder
		}
		redactedEntries = append(redactedEntries, redacted)
	}

	report := InspectTemplateReport{
		Kind:         "template",
		Name:         name,
		Driver:       res.Driver,
		ContentBytes: len(res.Content),
		Sources:      toInspectSourceReports(res.Sources),
		Entries:      redactedEntries,
	}

	if err := writeInspectOutput(stdout, format, report, formatTemplateReportText(report)); err != nil {
		fmt.Fprintf(stderr, "Inspect output failed: %v\n", err)
		return 1
	}
	return 0
}

func runInspectCommand(ctx context.Context, orch *engine.Orchestrator, cmdArgs []string, format string, stdout, stderr io.Writer) int {
	details, err := orch.BuildEnvForCommandDetails(ctx, engine.EnvConfig{
		CmdArgs:         cmdArgs,
		EmptyEnv:        true,
		RedactedPreview: true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "Inspect run failed: %v\n", err)
		return exitCodeForContext(ctx, 1)
	}

	redactedEnv := make(map[string]string, len(details.InjectedKeys))
	for _, k := range details.InjectedKeys {
		redactedEnv[k] = runner.RedactedPlaceholder
	}

	pathMaskTrie := runner.NewMaskTrie(details.MaskSecrets)
	maskStr := func(s string) string {
		if pathMaskTrie == nil || s == "" {
			return s
		}
		var buf bytes.Buffer
		mw := runner.NewMaskingWriter(&buf, pathMaskTrie)
		_, _ = mw.Write([]byte(s))
		_ = mw.Flush()
		return buf.String()
	}

	templates := make([]InspectTemplateDeliveryReport, 0, len(details.RenderedTemplates))
	for _, rt := range details.RenderedTemplates {
		mode := "path"
		ephemeral := !rt.Persist
		switch {
		case rt.EnvVar != "":
			mode = "env"
			ephemeral = false
		case rt.EnvPathVar != "" && rt.Path == "":
			mode = "env_path"
		case rt.EnvPathVar != "" && rt.Path != "":
			mode = "env_path+path"
		}
		templates = append(templates, InspectTemplateDeliveryReport{
			Template:  rt.TemplateName,
			Driver:    rt.Driver,
			Mode:      mode,
			Env:       rt.EnvVar,
			EnvPath:   rt.EnvPathVar,
			Path:      maskStr(rt.Path),
			Ephemeral: ephemeral,
			Persist:   rt.Persist,
		})
	}

	displayCmd := details.RawCommand
	if len(displayCmd) == 0 {
		displayCmd = cmdArgs
	}

	report := InspectRunReport{
		Kind:      "run",
		Command:   displayCmd,
		Env:       redactedEnv,
		Templates: templates,
	}

	if err := writeInspectOutput(stdout, format, report, formatRunReportText(report)); err != nil {
		fmt.Fprintf(stderr, "Inspect output failed: %v\n", err)
		return 1
	}
	return 0
}

func splitInspectRunArgs(args []string) (preArgs, postArgs []string) {
	seenRun := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-o" || a == "--output" {
			i++
			continue
		}
		if strings.HasPrefix(a, "-o=") || strings.HasPrefix(a, "--output=") || a == "-h" || a == "--help" {
			continue
		}
		if strings.HasPrefix(a, "-") {
			return args, nil
		}
		if !seenRun {
			if a == "run" || a == "command" {
				seenRun = true
				continue
			}
			return args, nil
		}
		return args[:i], args[i:]
	}
	return args, nil
}

func buildOverviewReport(cfg *config.Config) InspectOverviewReport {
	vaults := sortedMapKeys(cfg.Vaults)
	envs := sortedMapKeys(cfg.Envs)
	templates := sortedMapKeys(cfg.Templates)
	autoload := make([]string, 0, len(cfg.Autoload))
	for _, rule := range cfg.Autoload {
		autoload = append(autoload, rule.Match)
	}
	return InspectOverviewReport{
		Kind:      "overview",
		Vaults:    vaults,
		Envs:      envs,
		Templates: templates,
		Autoload:  autoload,
	}
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func toInspectSourceReports(sources []engine.MatchedEntrySummary) []InspectSourceReport {
	out := make([]InspectSourceReport, 0, len(sources))
	for _, s := range sources {
		out = append(out, InspectSourceReport{
			Vault:      s.Vault,
			Path:       s.Path,
			Title:      s.Title,
			Tags:       slices.Clone(s.Tags),
			OutputKeys: slices.Clone(s.OutputKeys),
			Traces:     slices.Clone(s.Traces),
		})
	}
	return out
}

func writeInspectOutput(w io.Writer, format string, structured any, text string) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(structured)
	case "yaml":
		data, err := utils.MarshalYAML(structured)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	default:
		_, err := io.WriteString(w, text)
		return err
	}
}

func formatOverviewText(r InspectOverviewReport) string {
	var b strings.Builder
	b.WriteString("Cloakenv Configuration Overview (redacted)\n")
	b.WriteString(strings.Repeat("-", 40) + "\n")
	fmt.Fprintf(&b, "Vaults (%d): %s\n", len(r.Vaults), formatListOrNone(r.Vaults))
	fmt.Fprintf(&b, "Envs (%d): %s\n", len(r.Envs), formatListOrNone(r.Envs))
	fmt.Fprintf(&b, "Templates (%d): %s\n", len(r.Templates), formatListOrNone(r.Templates))
	fmt.Fprintf(&b, "Autoload rules (%d): %s\n", len(r.Autoload), formatListOrNone(r.Autoload))
	return b.String()
}

func formatEnvReportText(r InspectEnvReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Env: %s\n", r.Name)
	formatSourcesText(&b, r.Sources)
	b.WriteString("Generated variables:\n")
	if len(r.Env) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, k := range sortedMapKeys(r.Env) {
			fmt.Fprintf(&b, "  %s=%s\n", k, runner.RedactedPlaceholder)
		}
	}
	return b.String()
}

func formatTemplateReportText(r InspectTemplateReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Template: %s (driver=%s, content_bytes=%d)\n", r.Name, r.Driver, r.ContentBytes)
	formatSourcesText(&b, r.Sources)
	fmt.Fprintf(&b, "Post-rules entries (%d):\n", len(r.Entries))
	if len(r.Entries) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for idx, entry := range r.Entries {
			fmt.Fprintf(&b, "  [%d]\n", idx)
			for _, k := range sortedMapKeys(entry) {
				fmt.Fprintf(&b, "    %s=%s\n", k, runner.RedactedPlaceholder)
			}
		}
	}
	return b.String()
}

func formatRunReportText(r InspectRunReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Command: %s\n", strings.Join(r.Command, " "))
	b.WriteString("Injected variables:\n")
	if len(r.Env) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, k := range sortedMapKeys(r.Env) {
			fmt.Fprintf(&b, "  %s=%s\n", k, runner.RedactedPlaceholder)
		}
	}
	b.WriteString("Templates:\n")
	if len(r.Templates) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, t := range r.Templates {
			var parts []string
			parts = append(parts, "driver="+t.Driver, "mode="+t.Mode)
			if t.Env != "" {
				parts = append(parts, "env="+t.Env)
			}
			if t.EnvPath != "" {
				parts = append(parts, "env_path="+t.EnvPath)
			}
			if t.Path != "" {
				parts = append(parts, "path="+t.Path)
			}
			if t.Mode != "env" {
				parts = append(parts, fmt.Sprintf("ephemeral=%t", t.Ephemeral), fmt.Sprintf("persist=%t", t.Persist))
			}
			fmt.Fprintf(&b, "  %s (%s)\n", t.Template, strings.Join(parts, ", "))
		}
	}
	return b.String()
}

func formatSourcesText(b *strings.Builder, sources []InspectSourceReport) {
	fmt.Fprintf(b, "Matched entries (%d):\n", len(sources))
	if len(sources) == 0 {
		b.WriteString("  (none)\n")
		return
	}
	for _, s := range sources {
		label := s.Title
		if label == "" {
			label = s.Path
		}
		if label == "" {
			label = "(unnamed)"
		}
		fmt.Fprintf(b, "  - [%s] %s", s.Vault, label)
		if len(s.Tags) > 0 {
			fmt.Fprintf(b, " tags=[%s]", strings.Join(s.Tags, ", "))
		}
		if len(s.OutputKeys) > 0 {
			fmt.Fprintf(b, " keys=[%s]", strings.Join(s.OutputKeys, ", "))
		}
		b.WriteString("\n")
	}
}

func formatListOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}
