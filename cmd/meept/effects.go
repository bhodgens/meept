package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newEffectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "effects",
		Short: "inspect and reconcile external-effect ledger records",
		Long: `Inspect and reconcile the external-effect idempotency ledger.

Irreversible external effects (push, git push) are claimed in a durable
ledger before execution. A crash between claim and completion leaves a
record the daemon surfaces on startup/resume; tools that cannot safely
re-execute wait for human reconciliation here.

Examples:
  meept effects list                          # pending claimed/receipted records
  meept effects list --state=completed        # filter by state
  meept effects list --json                   # machine-readable output
  meept effects reconcile <key>               # show record + how to fix
  meept effects reconcile <key> --complete    # human confirmed it landed
  meept effects reconcile <key> --abandon --reason 'provider never sent it'`,
	}

	cmd.AddCommand(newEffectsListCmd())
	cmd.AddCommand(newEffectsReconcileCmd())

	return cmd
}

func newEffectsListCmd() *cobra.Command {
	var (
		state  string
		asJSON bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "list effect ledger records",
		Long:  "List effect ledger records, oldest claimed first. Default filter is pending (claimed + receipted).",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer func() {
				if cerr := client.Close(); cerr != nil {
					fmt.Fprintf(os.Stderr, "effects: close daemon connection: %v\n", cerr)
				}
			}()

			params := map[string]any{}
			if state != "" {
				params["state"] = state
			}

			rawResult, err := client.Call("effects.list", params)
			if err != nil {
				if strings.Contains(err.Error(), "method not found") {
					return fmt.Errorf("effects ledger unavailable (daemon too old or ledger failed to open): %w", err)
				}
				return fmt.Errorf("effects: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if asJSON {
				output, err := json.MarshalIndent(resultMap, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(output))
				return nil
			}

			records, _ := resultMap["effects"].([]any)
			rows := make([]map[string]any, 0, len(records))
			for _, r := range records {
				if m, ok := r.(map[string]any); ok {
					rows = append(rows, m)
				}
			}
			renderEffectsTable(os.Stdout, rows)
			return nil
		},
	}

	cmd.Flags().StringVar(&state, "state", "", "Filter by state: claimed, receipted, completed, abandoned")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func newEffectsReconcileCmd() *cobra.Command {
	var (
		complete bool
		abandon  bool
		receipt  string
		reason   string
	)

	cmd := &cobra.Command{
		Use:   "reconcile <key>",
		Short: "reconcile one effect record",
		Long: `Reconcile one effect ledger record.

Exactly one of --complete / --abandon decides the record's fate:
  --complete          the effect verifiably landed; optionally attach a
                      hand-verified --receipt (only while the record is
                      still claimed)
  --abandon           the effect is dead; --reason is required and the
                      record stays for audit

With neither flag the current record is printed with a hint.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEffectsReconcile(args[0], complete, abandon, receipt, reason)
		},
	}

	cmd.Flags().BoolVar(&complete, "complete", false, "Mark the effect completed (human confirmed it landed)")
	cmd.Flags().BoolVar(&abandon, "abandon", false, "Mark the effect abandoned (dead; kept for audit)")
	cmd.Flags().StringVar(&receipt, "receipt", "", "JSON receipt to record before completing (only valid while claimed)")
	cmd.Flags().StringVar(&reason, "reason", "", "Why the effect is being abandoned (required with --abandon)")

	return cmd
}

// runEffectsReconcile drives effects.reconcile. Flag validation happens
// before any daemon connection so bad invocations fail fast offline.
func runEffectsReconcile(key string, complete, abandon bool, receipt, reason string) error {
	if !complete && !abandon {
		return showEffectRecord(key)
	}
	if complete && abandon {
		return fmt.Errorf("effects: use exactly one of --complete or --abandon")
	}
	if abandon && strings.TrimSpace(reason) == "" {
		return fmt.Errorf("effects: --reason is required with --abandon")
	}

	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "effects: close daemon connection: %v\n", cerr)
		}
	}()

	params := map[string]any{"key": key}
	var action string
	if complete {
		action = "complete"
		if receipt != "" {
			params["receipt"] = json.RawMessage(receipt)
		}
	} else {
		action = "abandon"
		params["reason"] = reason
	}
	params["action"] = action

	rawResult, err := client.Call("effects.reconcile", params)
	if err != nil {
		if strings.Contains(err.Error(), "method not found") {
			return fmt.Errorf("effects ledger unavailable (daemon too old or ledger failed to open): %w", err)
		}
		return fmt.Errorf("effects: %w", err)
	}

	var resultMap map[string]any
	if err := json.Unmarshal(rawResult, &resultMap); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	record, ok := resultMap["record"].(map[string]any)
	if !ok {
		return fmt.Errorf("effects: unexpected reconcile response (no record)")
	}

	fmt.Printf("effect %s reconciled: %s\n", getStringOr(record, "key", key), getStringOr(record, "state", action+"d"))
	return nil
}

// showEffectRecord fetches the record for key via effects.list and prints
// it with the reconciliation hint, exit 0.
func showEffectRecord(key string) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "effects: close daemon connection: %v\n", cerr)
		}
	}()

	rawResult, err := client.Call("effects.list", map[string]any{"key": key})
	if err != nil {
		if strings.Contains(err.Error(), "method not found") {
			return fmt.Errorf("effects ledger unavailable (daemon too old or ledger failed to open): %w", err)
		}
		return fmt.Errorf("effects: %w", err)
	}
	var resultMap map[string]any
	if err := json.Unmarshal(rawResult, &resultMap); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	records, _ := resultMap["effects"].([]any)
	if len(records) == 0 {
		return fmt.Errorf("effects: unknown effect key %s", key)
	}
	record, ok := records[0].(map[string]any)
	if !ok {
		return fmt.Errorf("effects: unexpected list response")
	}

	renderEffectRecord(os.Stdout, record)
	fmt.Println()
	fmt.Println("confirm the effect's real outcome, then re-run with --complete [--receipt '<json>'] or --abandon --reason '<text>'")
	return nil
}

// renderEffectsTable writes the list table: KEY STATE TOOL CLAIMED AGE.
// Effect keys are identities and the copy-paste target for reconcile, so
// they are printed in full — never truncated.
func renderEffectsTable(w io.Writer, records []map[string]any) {
	if len(records) == 0 {
		fmt.Fprintln(w, "no effects found")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "KEY\tSTATE\tTOOL\tCLAIMED\tAGE")
	for _, rec := range records {
		key := getStringOr(rec, "key", "")
		state := getStringOr(rec, "state", "")
		tool := getStringOr(rec, "tool", "")
		claimedAt := getStringOr(rec, "claimed_at", "")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", key, state, tool, formatDate(claimedAt), humanizeAge(parseTime(claimedAt)))
	}
	tw.Flush()
}

// renderEffectRecord prints one record field-by-field (lowercase labels).
func renderEffectRecord(w io.Writer, rec map[string]any) {
	fmt.Fprintf(w, "key:                 %s\n", getStringOr(rec, "key", ""))
	fmt.Fprintf(w, "state:               %s\n", getStringOr(rec, "state", ""))
	fmt.Fprintf(w, "tool:                %s\n", getStringOr(rec, "tool", ""))
	fmt.Fprintf(w, "provider_idempotent: %v\n", rec["provider_idempotent"] == true)
	fmt.Fprintf(w, "claimed_at:          %s\n", getStringOr(rec, "claimed_at", ""))
	if v, ok := getStringPtr(rec, "executed_at"); ok {
		fmt.Fprintf(w, "executed_at:         %s\n", v)
	}
	if v, ok := getStringPtr(rec, "completed_at"); ok {
		fmt.Fprintf(w, "completed_at:        %s\n", v)
	}
	if v, ok := getStringPtr(rec, "receipt"); ok {
		fmt.Fprintf(w, "receipt:             %s\n", v)
	}
	if v, ok := getStringPtr(rec, "payload"); ok {
		fmt.Fprintf(w, "payload:             %s\n", v)
	}
}

func getStringPtr(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		if t == "" {
			return "", false
		}
		return t, true
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b), true
		}
		return "", false
	}
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

func formatDate(s string) string {
	t := parseTime(s)
	if t.IsZero() {
		return s
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// humanizeAge renders a humanized, lowercase age since at ("3h12m"); a
// zero/future time renders "unknown".
func humanizeAge(at time.Time) string {
	if at.IsZero() || at.After(time.Now()) {
		return "unknown"
	}
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}
