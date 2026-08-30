// Command eval runs oracle checks against a workdir and stores pass^k run
// records under <state-dir>/eval/. See docs/plans/20260829-harness-eval/02-eval-cli.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/eval"
)

// evalListLimit caps the number of runs returned by list.
const evalListLimit = 50

// defaultEvalTimeout bounds each oracle invocation; non-zero so a hung
// command cannot wedge the CLI forever.
const defaultEvalTimeout = 10 * time.Minute

// errEvalNotPassed signals a run that completed but did not pass; it maps to
// exit code 1 (distinct from usage errors, which map to exit code 2).
var errEvalNotPassed = errors.New("eval run did not pass")

// evalUsageError marks argument/flag misuse so callers can map it to exit
// code 2 (as opposed to 1 for a genuine failure such as a failed eval run).
type evalUsageError struct{ err error }

func (e *evalUsageError) Error() string { return e.err.Error() }
func (e *evalUsageError) Unwrap() error { return e.err }

// EvalRunOptions parameterizes runEvalRun. StoreDir holds the eval record
// directory (injected so tests never touch $HOME); Workdir is passed to the
// oracle verbatim — nothing resolves or falls back to the process CWD.
type EvalRunOptions struct {
	TaskID   string
	ModelID  string
	K        int
	Command  string
	Workdir  string
	Timeout  time.Duration
	StoreDir string
}

// evalStateDir resolves the eval store root: explicit override first, then
// the global --state-dir flag, then $HOME/.meept.
func evalStateDir(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if stateDir != "" {
		return filepath.Join(stateDir, "eval"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".meept", "eval"), nil
}

// evalStore is the on-disk RunRecord store: one JSON file per record at
// <dir>/<id>.json.
type evalStore struct{ dir string }

// save persists r as <dir>/<id>.json, creating the directory if needed.
func (s evalStore) save(r *eval.RunRecord) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create eval dir: %w", err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal eval run: %w", err)
	}
	path := filepath.Join(s.dir, r.ID+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil { //nolint:gosec // record file is non-sensitive
		return fmt.Errorf("write eval run: %w", err)
	}
	return nil
}

// list returns up to evalListLimit records, newest-first by CreatedAt.
// Unreadable/corrupt files are skipped, not fatal.
func (s evalStore) list() ([]*eval.RunRecord, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read eval dir: %w", err)
	}

	var records []*eval.RunRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		var rec eval.RunRecord
		if err := json.Unmarshal(data, &rec); err != nil || rec.ID == "" {
			continue
		}
		records = append(records, &rec)
	}

	sort.SliceStable(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	if len(records) > evalListLimit {
		records = records[:evalListLimit]
	}
	return records, nil
}

// get loads the record with the given id, or an error when it is absent.
// Tolerates an id passed with a .json suffix (show's shell-completion UX).
func (s evalStore) get(id string) (*eval.RunRecord, error) {
	id = strings.TrimSuffix(id, ".json")
	if id == "" || strings.ContainsRune(id, filepath.Separator) {
		return nil, fmt.Errorf("eval run not found: %s", id)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return nil, fmt.Errorf("eval run not found: %s", id)
	}
	var rec eval.RunRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("eval run not found: %s", id)
	}
	return &rec, nil
}

// handleEvalError distinguishes usage misuse (exit 2) from eval failure
// (exit 1): nil passes through, errEvalNotPassed maps to exit 1, and any
// other error is returned as a normal RunE error (exit 1 via rootCmd).
func handleEvalError(err error) error {
	if err == nil {
		return nil
	}
	var usage *evalUsageError
	if errors.As(err, &usage) {
		fmt.Fprintln(os.Stderr, "Error:", usage.Error())
		os.Exit(2)
	}
	if errors.Is(err, errEvalNotPassed) {
		os.Exit(1)
	}
	return err
}

// runEvalRun executes a shell oracle k times in opts.Workdir, records a
// pass^k RunRecord in the store, and prints its id and verdict.
func runEvalRun(ctx context.Context, w io.Writer, opts EvalRunOptions) error {
	if opts.Workdir == "" || opts.Command == "" {
		return &evalUsageError{err: fmt.Errorf("--workdir and --command are required")}
	}

	dir, err := evalStateDir(opts.StoreDir)
	if err != nil {
		return err
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultEvalTimeout
	}

	oracle := eval.ShellOracle{
		OracleName: "cli",
		Command:    opts.Command,
		Timeout:    timeout,
	}

	record := eval.NewRun(eval.KindPassK, opts.TaskID, opts.ModelID, opts.K)
	record.HarnessHash = eval.HarnessHash("cli", "", "")
	for i := 0; i < opts.K; i++ {
		res, err := oracle.Check(ctx, opts.Workdir)
		if err != nil {
			return fmt.Errorf("oracle attempt %d failed: %w", i+1, err)
		}
		record.AddAttempt(eval.Attempt{
			Index:   i,
			ModelID: opts.ModelID,
			Passed:  res.Passed,
			Oracle:  res,
		})
	}
	record.Passed = eval.PassK(record.Attempts, opts.K)
	record.OracleName = oracle.Name()

	store := evalStore{dir: dir}
	if err := store.save(record); err != nil {
		return err
	}

	fmt.Fprintf(w, "id: %s\n", record.ID)
	fmt.Fprintf(w, "passed: %t\n", record.Passed)
	if !record.Passed {
		// Completed but failed: sentinel maps to exit code 1 at the root.
		return errEvalNotPassed
	}
	return nil
}

// runEvalShow prints the raw JSON of one stored run.
func runEvalShow(ctx context.Context, w io.Writer, storeDir, id string) error {
	dir, err := evalStateDir(storeDir)
	if err != nil {
		return err
	}
	rec, err := (evalStore{dir: dir}).get(id)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal eval run: %w", err)
	}
	fmt.Fprintln(w, string(data))
	return nil
}

// runEvalList prints stored runs newest-first, one line each.
func runEvalList(ctx context.Context, w io.Writer, storeDir string) error {
	dir, err := evalStateDir(storeDir)
	if err != nil {
		return err
	}
	records, err := (evalStore{dir: dir}).list()
	if err != nil {
		return err
	}
	for _, rec := range records {
		fmt.Fprintf(w, "%s  %s  %s  passed=%t k=%d\n",
			rec.ID, rec.CreatedAt.Format(time.RFC3339), rec.TaskID, rec.Passed, rec.K)
	}
	return nil
}

func newEvalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "run oracle checks and score pass^k",
		Long:  "run oracle checks against a workdir and store pass^k run records.",
	}

	cmd.AddCommand(newEvalRunCmd())
	cmd.AddCommand(newEvalShowCmd())
	cmd.AddCommand(newEvalListCmd())

	return cmd
}

func newEvalRunCmd() *cobra.Command {
	var (
		taskID  string
		modelID string
		k       int
		command string
		workdir string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "run an oracle command k times and record pass^k",
		Long:  "run an oracle command k times sequentially in --workdir and record the pass^k result.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleEvalError(runEvalRun(cmd.Context(), os.Stdout, EvalRunOptions{
				TaskID:  taskID,
				ModelID: modelID,
				K:       k,
				Command: command,
				Workdir: workdir,
			}))
		},
	}

	cmd.Flags().StringVar(&taskID, "task", "", "task identifier")
	cmd.Flags().StringVar(&modelID, "model", "", "model identifier")
	cmd.Flags().IntVar(&k, "k", 1, "number of consecutive oracle passes required")
	cmd.Flags().StringVar(&command, "command", "", "shell command to run as the oracle (required)")
	cmd.Flags().StringVar(&workdir, "workdir", "", "directory the oracle command runs in (required)")

	return cmd
}

func newEvalShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <run-id>",
		Short: "print one eval run as json",
		Long:  "print the raw json record of a single eval run.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleEvalError(runEvalShow(cmd.Context(), os.Stdout, "", strings.TrimSpace(args[0])))
		},
	}
}

func newEvalListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list stored eval runs, newest first",
		Long:  "list stored eval runs newest-first, capped at the most recent 50.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleEvalError(runEvalList(cmd.Context(), os.Stdout, ""))
		},
	}
}
