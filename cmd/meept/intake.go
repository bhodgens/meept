package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/config"
)

// newIntakeCmd builds the `meept intake` command tree.
func newIntakeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "intake",
		Short: "GitHub ticket intake diagnostics (report-only)",
		Long:  "Inspect the [intake] config block the daemon uses to schedule the ticket->goal poller.\nAll subcommands are report-only (doctor convention): nothing is mutated.",
	}
	cmd.AddCommand(newIntakeStatusCmd())
	return cmd
}

// newIntakeStatusCmd builds `meept intake status`: enabled?, watch repo,
// poll interval, labels, default employee, gate command, project number.
// The GitHub token is NEVER printed (intake rides the [github] token
// sources; report the presence only via meept github status).
func newIntakeStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report the [intake] config state (report-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIntakeStatus()
		},
	}
}

// runIntakeStatus prints the report. Always exit 0: a disabled state is
// the shipped default, not a failure — the same report-only posture as
// meept github status / meept doctor.
func runIntakeStatus() error {
	cfg, err := config.LoadDefault()
	if err != nil {
		// A config load failure still yields a useful report: the block
		// is effectively disabled.
		fmt.Printf("meept intake status\n-------------------\n")
		fmt.Printf("  config:      %s\n", err)
		fmt.Printf("  enabled:     false (config unavailable; no poller is scheduled)\n")
		return nil
	}

	i := cfg.Intake
	fmt.Printf("meept intake status\n-------------------\n")
	fmt.Printf("  enabled:     %v\n", i.Enabled)

	if !i.Enabled {
		fmt.Printf("  poller:      0 — intake disabled (default); no poll job is scheduled\n")
		fmt.Printf("  note:        intake reuses the [github] token sources; enable [github] with a token_secret too\n")
		return nil
	}

	// Watch repo.
	fmt.Printf("  watch_repo:  %s\n", valueOrDash(i.WatchRepo))

	// Poll interval (echo the configured string; show the wiring default
	// when unset — the daemon parses the same way).
	interval := i.PollInterval
	if interval == "" {
		interval = "5m (default)"
	}
	fmt.Printf("  interval:    %s\n", interval)

	// Labels.
	if len(i.RequiredLabels) > 0 {
		fmt.Printf("  labels:      required %s\n", strings.Join(i.RequiredLabels, ", "))
	} else {
		fmt.Printf("  labels:      none (empty required_labels matches every issue)\n")
	}
	fmt.Printf("  done_label:  %s\n", valueOrDash(i.DoneLabel))

	// Default employee: resolved-or-missing.
	if emp := strings.TrimSpace(i.DefaultEmployee); emp != "" {
		fmt.Printf("  employee:    %s (goals are created for this id; it need not exist yet)\n", emp)
	} else {
		fmt.Printf("  employee:    MISSING — the daemon fails closed (no poller) without default_employee\n")
	}

	// Gate command.
	if i.GateCommand != "" {
		fmt.Printf("  gate:        %s\n", i.GateCommand)
	} else {
		fmt.Printf("  gate:        none (created goals have no quality gate)\n")
	}

	// Project projection.
	if i.ProjectNumber > 0 {
		fmt.Printf("  project:     #%d (GitHub Projects V2 projection)\n", i.ProjectNumber)
	} else {
		fmt.Printf("  project:     none (project_number=0 disables projection)\n")
	}

	// Validity preview: mirror the daemon's ValidateEnabled fail-closed
	// posture so the operator sees the outcome before booting.
	if err := i.ValidateEnabled(); err != nil {
		fmt.Printf("  validity:    INVALID — %s (the daemon logs and schedules nothing)\n", err)
	} else if cfg.GitHub.TokenSecret == "" {
		fmt.Printf("  validity:    config ok, but [github].token_secret is empty — every poll will fail until a token is configured\n")
	} else {
		fmt.Printf("  validity:    ok — the daemon schedules the intake_poller job on boot\n")
	}
	return nil
}
