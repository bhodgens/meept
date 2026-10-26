// Command fakeserver is the runtime-hang e2e suite's fake llama-server.
//
// It mimics the production failure mode the llama-hang-watchdog tree
// detects: the HTTP surface stays alive (/health and /props answer 200
// forever) while the inference path is wedged (/v1/chat/completions blocks
// and never responds).
//
// Phase control is FILE-based, not per-process: the daemon's restart path
// (RuntimeManager.RestartProvider) re-runs the SAME spawn_command, so a
// pure "--wedge-completions blocks until exit" flag would wedge every
// respawned generation too and the suite could never observe recovery.
// With --wedge-completions the server blocks completions only WHILE the
// --wedge-file exists; deleting the file makes the next completion request
// answer normally (a fresh process serves everything normally either way,
// since the wedged request only blocks its own goroutine).
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	port := flag.Int("port", 0, "loopback port to serve (required)")
	wedge := flag.Bool("wedge-completions", false, "block /v1/chat/completions while the wedge file exists")
	wedgeFile := flag.String("wedge-file", "", "control file: completions block while it exists (with --wedge-completions)")
	selfPIDFile := flag.String("self-pid-file", "", "write this process's pid here at startup (restart detection)")
	flag.Parse()

	if *port == 0 {
		fmt.Fprintln(os.Stderr, "fakeserver: --port is required")
		os.Exit(2)
	}
	if *selfPIDFile != "" {
		if err := os.WriteFile(*selfPIDFile, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fakeserver: write self pid file: %v\n", err)
			os.Exit(2)
		}
	}

	wedged := func() bool {
		if !*wedge || *wedgeFile == "" {
			return false
		}
		_, err := os.Stat(*wedgeFile)
		return err == nil
	}

	// Released only by process exit: a wedged completion blocks here until
	// the supervisor SIGTERMs this server (the real-world hang shape).
	blockForever := make(chan struct{})

	mux := http.NewServeMux()
	ok := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}
	}
	mux.HandleFunc("/health", ok(`{"status":"ok"}`))
	mux.HandleFunc("/props", ok(`{"default_generation_settings":{"n_ctx":2048},"total_slots":1}`))
	mux.HandleFunc("/v1/models", ok(`{"object":"list","data":[{"id":"stub","object":"model"}]}`))
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if wedged() {
			fmt.Printf("fakeserver[%d]: completion WEDGED (blocking)\n", os.Getpid())
			<-blockForever
			return
		}
		fmt.Printf("fakeserver[%d]: completion served\n", os.Getpid())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"fake","object":"chat.completion","created":0,"model":"stub","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	})

	addr := "127.0.0.1:" + strconv.Itoa(*port)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "fakeserver: serve: %v\n", err)
			os.Exit(1)
		}
	}()
	fmt.Printf("fakeserver[%d]: listening on %s wedge=%v\n", os.Getpid(), addr, *wedge)

	// Exit cleanly on SIGTERM/SIGINT so the daemon's restart path reaps
	// this process deterministically (blocked handler goroutines die with
	// the process).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	fmt.Printf("fakeserver[%d]: terminating\n", os.Getpid())
}
