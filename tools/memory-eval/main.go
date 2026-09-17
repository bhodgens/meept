// memory-eval grades how well local llama.cpp models perform meept's memory
// extraction: ambient epistemic extraction (claims/decisions/predictions from
// conversation) and distillation (lessons/procedures). It drives the same
// injectable seams production uses (memory.AmbientClassifierLLM and
// memory.DistillSummarizer) over an OpenAI-compatible endpoint, with NO
// grammar constraint — grading the model, not the grammar.
//
// Usage:
//
//	go run ./tools/memory-eval --endpoint http://127.0.0.1:8090/v1 \
//	    --model /Volumes/LLMs/.../model.gguf [--mode ambient|distill|both] \
//	    [--corpus tools/memory-eval/corpus.json] [--out metrics.json]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	endpoint := flag.String("endpoint", "http://127.0.0.1:8090/v1", "OpenAI-compatible chat endpoint base URL")
	model := flag.String("model", "", "model id string sent to the endpoint")
	corpusPath := flag.String("corpus", "tools/memory-eval/corpus.json", "gold corpus path")
	outPath := flag.String("out", "", "metrics JSON output path (default: stdout only)")
	mode := flag.String("mode", "both", "ambient | distill | both")
	validateOnly := flag.Bool("validate-corpus", false, "validate the corpus and print counts, then exit")
	grammarFile := flag.String("grammar-file", "", "GBNF grammar file attached to every request (llama.cpp wire grammar; measures the constrained path)")
	matchThreshold := flag.Float64("match-threshold", 0.6, "token-overlap threshold for a gold match")
	judge := flag.Bool("judge", false, "enable the LLM judge lane: unmatched candidates get a semantic second opinion (yes/no) from the model")
	judgeEndpoint := flag.String("judge-endpoint", "", "OpenAI-compatible endpoint for the judge lane (default: same as --endpoint)")
	timeout := flag.Duration("timeout", 120*time.Second, "per-call timeout")
	flag.Parse()

	corpus, err := loadCorpus(*corpusPath)
	if err != nil {
		fatal("load corpus: %v", err)
	}
	if err := validateCorpus(corpus); err != nil {
		fatal("corpus invalid: %v", err)
	}
	printCorpusSummary(corpus)
	if *validateOnly {
		return
	}
	if *model == "" {
		fatal("--model is required (the id string the endpoint expects)")
	}

	client := newChatClient(*endpoint, *model, *timeout)
	if *grammarFile != "" {
		gb, err := os.ReadFile(*grammarFile)
		if err != nil {
			fatal("read grammar: %v", err)
		}
		client.setGrammar(string(gb))
	}
	fmt.Printf("\nEvaluating model: %s\nEndpoint: %s\nMode: %s\nThreshold: %.2f\n\n",
		*model, *endpoint, *mode, *matchThreshold)

	conds := map[string]string{"grammar": "none", "temperature": "0.2", "max_tokens": "1024"}
	if *grammarFile != "" {
		conds["grammar"] = *grammarFile
	}
	result := EvalResult{
		Model:      *model,
		Endpoint:   *endpoint,
		Mode:       *mode,
		Threshold:  *matchThreshold,
		RunAt:      time.Now().UTC().Format(time.RFC3339),
		Conditions: conds,
	}

	if *mode == "ambient" || *mode == "both" {
		var judgePairer *judgePairer
		if *judge {
			je := *judgeEndpoint
			if je == "" {
				je = *endpoint
			}
			judgeClient := newChatClient(je, *model, *timeout)
			judgePairer = newJudgePairer(judgeClient)
		}
		result.Ambient = runAmbientEvalJudge(client, corpus, *matchThreshold, judgePairer)
	}
	if *mode == "distill" || *mode == "both" {
		result.Distill = runDistillEval(client, corpus, *matchThreshold)
	}

	printSummary(result)
	if *outPath != "" {
		if err := writeJSON(*outPath, result); err != nil {
			fatal("write metrics: %v", err)
		}
		fmt.Printf("metrics written: %s\n", *outPath)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "memory-eval: "+format+"\n", args...)
	os.Exit(1)
}

func writeJSON(path string, v any) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}
