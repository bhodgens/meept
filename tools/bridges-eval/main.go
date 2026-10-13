// bridges-eval runs the B05 bridges-on/off cross-session recall evaluation
// (branch 09, P10 gate) against a committed synthetic fixture. The default
// mode is hermetic: bridge generation/verification run through scripted
// deterministic seams over the REAL pipeline (memory.GenerateBridges /
// memory.VerifyBridges), recall applies the REAL balancing seam
// (vector.SelectBalanced) for arm B, and grading is a mock grader over
// injected context. --live switches the answer lane to a local
// OpenAI-compatible endpoint (e.g. llama-server on 127.0.0.1:8080) — never
// required; live-model runs are a follow-up.
//
// Usage:
//
//	go run ./tools/bridges-eval --arm=both \
//	    --fixture tools/bridges-eval/fixtures/bridges-fixture.json \
//	    --out docs/research/eval/bridges-eval.json
//
// The runner is idempotent and resumable: per-(question, arm) rows in the
// output artifact with a matching fixture+config hash are reused on re-run.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/eval"
)

func main() {
	fixturePath := flag.String("fixture", "tools/bridges-eval/fixtures/bridges-fixture.json", "committed synthetic fixture path")
	outPath := flag.String("out", "docs/research/eval/bridges-eval.json", "report artifact path")
	arm := flag.String("arm", "both", "which arm(s) to run: a | b | both")
	kb := flag.Int("kb", eval.DefaultKB, "arm-B bridge cap (memory.bridges.kb)")
	topK := flag.Int("top-k", eval.DefaultTopK, "context window: units injected per query")
	threshold := flag.Float64("match-threshold", eval.DefaultMatchThresh, "token-overlap threshold for an expected fact to count as covered")
	live := flag.Bool("live", false, "answer via a local OpenAI-compatible endpoint instead of the mock grader (follow-up lane; never required)")
	endpoint := flag.String("endpoint", "http://127.0.0.1:8080/v1", "chat endpoint base URL for --live")
	model := flag.String("model", "", "model id sent to the endpoint when --live")
	timeout := flag.Duration("timeout", 120*time.Second, "per-question timeout")
	flag.Parse()

	arm = normalizeArm(arm)

	fx, err := eval.LoadFixture(*fixturePath)
	if err != nil {
		fatal("%v", err)
	}
	fHash, err := eval.HashBytesFile(*fixturePath)
	if err != nil {
		fatal("fixture hash: %v", err)
	}
	cfg := eval.BridgesEvalConfig{
		KB:             *kb,
		TopK:           *topK,
		MatchThreshold: *threshold,
		BridgeDFMax:    eval.DefaultBridgeDFMax,
		Verify:         true,
		Live:           *live,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	rep := eval.LoadReport(*outPath, fHash, cfg.ConfigHash())
	rep.RunAt = time.Now().UTC().Format(time.RFC3339)
	rep.Mode = modeLabel(*live)
	rep.Note = "synthetic hermetic run (scripted generator/verifier + mock grader). Live-model runs against the local llama-server stack are a follow-up; use --live to enable."
	rep.FixturePath = *fixturePath

	var answer eval.AnswerFunc
	if *live {
		answer, err = eval.NewLiveAnswerFunc(*endpoint, *model, *timeout)
		if err != nil {
			fatal("live answer lane: %v", err)
		}
	} else {
		answer = eval.SynthesizeAnswer
	}

	// Arm B needs the bridge pool; arm A runs without bridges. Build once.
	var poolInfo eval.BridgePoolInfo
	var bridgeRecords []eval.BridgeEvalBridgeRecord
	if *arm == "b" || *arm == "both" {
		records, info, berr := eval.BuildBridgesForRun(ctx, fx, cfg)
		if berr != nil {
			fatal("%v", berr)
		}
		bridgeRecords = records
		poolInfo = info
	}

	if *arm == "a" || *arm == "both" {
		rows, rerr := eval.RunArm(ctx, fx, cfg, "a", nil, answer, eval.RowMap(rep.Questions, "a"))
		if rerr != nil {
			fatal("arm a: %v", rerr)
		}
		agg := eval.AggregateRows("a", rows, 0)
		if werr := eval.WriteReport(*outPath, rep, rows, &agg, nil, poolInfo); werr != nil {
			fatal("write: %v", werr)
		}
		fmt.Printf("arm a: %d questions, correctness %.2f%%, mean context tokens %.1f\n",
			agg.Questions, agg.CorrectnessPct, agg.MeanContextTokens)
	}
	if *arm == "b" || *arm == "both" {
		rows, rerr := eval.RunArm(ctx, fx, cfg, "b", bridgeRecords, answer, eval.RowMap(rep.Questions, "b"))
		if rerr != nil {
			fatal("arm b: %v", rerr)
		}
		aggB := eval.AggregateRows("b", rows, poolInfo.Generated)
		var aggA *eval.ArmAggregate
		if a, ok := rep.Arms["a"]; ok {
			aggA = &a
		}
		if werr := eval.WriteReport(*outPath, rep, rows, aggA, &aggB, poolInfo); werr != nil {
			fatal("write: %v", werr)
		}
		fmt.Printf("arm b: %d questions, correctness %.2f%%, mean context tokens %.1f, bridges included %d (avg %.2f/query)\n",
			aggB.Questions, aggB.CorrectnessPct, aggB.MeanContextTokens, aggB.BridgesIncluded, aggB.BridgesIncludedAvg)
	}

	// Final summary (re-read so --arm a on a resumed artifact still prints
	// the gate when both arms are present).
	final, err := eval.ReadReport(*outPath)
	if err != nil {
		fatal("re-read: %v", err)
	}
	eval.PrintSummary(final)
}

func normalizeArm(arm *string) *string {
	v := strings.ToLower(strings.TrimSpace(*arm))
	switch v {
	case "a", "b", "both":
	default:
		fatal("--arm must be a|b|both, got %q", *arm)
	}
	*arm = v
	return arm
}

func modeLabel(live bool) string {
	if live {
		return "live"
	}
	return "synthetic"
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "bridges-eval: "+format+"\n", args...)
	os.Exit(1)
}
