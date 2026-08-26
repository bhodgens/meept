package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/spf13/cobra"
)

// modelClient abstracts the modelstore + runtime for testability.
type modelClient interface {
	List() []llm.ModelRecord
	Get(name string) (llm.ModelRecord, bool)
	Pull(ctx context.Context, repoID, quant string, progress func(done, total int64)) (*llm.ModelRecord, error)
}

func newModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "manage local GGUF models",
	}
	cmd.AddCommand(newModelPullCmd(), newModelListCmd(), newModelTestCmd())
	return cmd
}

func newModelStore() (*llm.ModelStore, error) {
	dir := os.Getenv("MEEPT_MODELS_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home: %w", err)
		}
		dir = home + "/.meept/models"
	}
	return llm.OpenModelStore(dir)
}

func newModelPullCmd() *cobra.Command {
	var quant string
	var force bool
	cmd := &cobra.Command{
		Use:   "pull <repo-id>",
		Short: "pull a gguf model from hugging face",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := newModelStore()
			if err != nil {
				return err
			}
			return runModelPull(cmd.Context(), store, args[0], quant, force, os.Stdout)
		},
	}
	cmd.Flags().StringVar(&quant, "quant", "", "quant substring to match (e.g. q4_k_m)")
	cmd.Flags().BoolVar(&force, "force", false, "re-download even if already present")
	return cmd
}

func newModelListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list locally pulled models",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := newModelStore()
			if err != nil {
				return err
			}
			return runModelList(store, asJSON, os.Stdout)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as json")
	return cmd
}

func newModelTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test <name>",
		Short: "run a one-token completion probe through the local runtime",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := newModelStore()
			if err != nil {
				return err
			}
			return runModelTest(cmd.Context(), store, args[0], nil, os.Stdout)
		},
	}
	return cmd
}

// probeModel performs a minimal completion against the local llama-server
// endpoint used by the runtime manager's local-models provider.
func probeModel(rec llm.ModelRecord) error {
	payload := map[string]any{
		"model":      rec.Name,
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode probe: %w", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://127.0.0.1:8080/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("probe: is the local runtime running? %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "model: close probe body: %v\n", cerr)
		}
	}()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("probe failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("probe: drain body: %w", err)
	}
	return nil
}

// humanBytes formats n in binary units with lowercase unit letters ("100 b",
// "2.0 Kb"). Output stays lowercase regardless of capitalization style.
func humanBytes(n int64) string {
	const unit = 1024
	b := "b"
	if n < unit {
		return fmt.Sprintf("%d %s", n, b)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	units := map[int64]string{0: "K", 1: "M", 2: "G", 3: "T", 4: "P", 5: "E"}
	return fmt.Sprintf("%.1f %sb", float64(n)/float64(div), units[int64(exp)])
}

// runModelPull downloads a model, writing progress to w.
func runModelPull(ctx context.Context, store modelClient, repoID, quant string, force bool, w io.Writer) error {
	last := struct{ done, total int64 }{}
	rec, err := store.Pull(ctx, repoID, quant, func(done, total int64) {
		if total > 0 && done != last.done {
			last.done, last.total = done, total
			pct := int(100 * done / total)
			fmt.Fprintf(w, "\rdownloading: %d%% (%s / %s)", pct, humanBytes(done), humanBytes(total))
		}
	})
	if err != nil {
		fmt.Fprintln(w)
		return err
	}
	fmt.Fprintln(w)
	if force {
		fmt.Fprintf(w, "pulled %s (%s)\n", rec.Name, humanBytes(rec.Bytes))
		return nil
	}
	fmt.Fprintf(w, "pulled %s -> %s\n", rec.Name, rec.File)
	return nil
}

// runModelList prints the local model inventory (json or table).
func runModelList(store modelClient, asJSON bool, w io.Writer) error {
	recs := store.List()
	if asJSON {
		data, err := json.MarshalIndent(recs, "", "  ")
		if err != nil {
			return err
		}
		_, werr := w.Write(data)
		return werr
	}
	if len(recs) == 0 {
		fmt.Fprintln(w, "no local models; use 'meept model pull <repo-id>'")
		return nil
	}
	for _, r := range recs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Name, r.RepoID, humanBytes(r.Bytes), r.AddedAt.Format("2006-01-02"))
	}
	return nil
}

// runModelTest probes a model with a one-token completion.
func runModelTest(ctx context.Context, store modelClient, name string, probe modelProbeFunc, w io.Writer) error {
	rec, ok := store.Get(name)
	if !ok {
		return fmt.Errorf("model %q not found", name)
	}
	start := time.Now()
	if probe != nil {
		d, err := probe(ctx, "http://127.0.0.1:8080", rec.Name)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "ok: %s responded in %s\n", rec.Name, d.Round(time.Millisecond))
		return nil
	}
	if err := probeModel(rec); err != nil {
		return err
	}
	fmt.Fprintf(w, "ok: %s responded in %s\n", rec.Name, time.Since(start).Round(time.Millisecond))
	return nil
}

// modelProbeFunc performs a completion probe; injectable in tests.
type modelProbeFunc func(ctx context.Context, baseURL, model string) (time.Duration, error)
