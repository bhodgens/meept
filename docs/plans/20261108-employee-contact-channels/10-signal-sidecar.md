# Signal sidecar (json-rpc client + supervisor) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The failing tests ALREADY EXIST and
> are committed (test gate 00T) - your job is to make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test (report suspected-wrong
> gate tests to the orchestrator instead). Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** internal/signal package: signal-cli subprocess supervisor + JSON-RPC 2.0 client over unix socket (contract C3). No legs yet - leaf 11 builds legs on this client.
- **Dependencies:** none (00T gate RED tests exist)
- **Estimated Context:** 60K
- **Concurrency Group:** A

## Goal

A supervised signal-cli json-rpc daemon per Signal account, plus a Go client exposing call control (startCall/acceptCall/hangupCall/listCalls/subscribeCallEvents + callEvent notifications) and messaging (send/typing + envelope notifications). The client is the transport seam both Signal channels ride.

## Context

signal-cli (https://github.com/AsamK/signal-cli) >= v0.13.0 runs `signal-cli -u +NUMBER jsonRpc --socket <path>`: a JSON-RPC 2.0 server on a unix socket. Requests carry incrementing ids; notifications (no id) arrive interleaved - `callEvent` for call state changes, `envelope` for inbound messages. Voice calling merged April 2026 (PR #1932) via a Rust helper (`signal-call-tunnel`, RingRTC); call audio can route over a socket (socket-audio mode) instead of host audio devices - the exact socket framing lives in the signal-call-tunnel Rust source, not the docs. One process per Signal account; treat one live call per account as the operating point. External research archive: .research/signal-telnyx-channels.md in this tree directory (read it for command/payload detail - do NOT re-derive from the web).

## Verified Anchors

- internal/voice/childproc.go:116-211 - TextServer/token handshake pattern (reference for supervised child I/O discipline)
- internal/llm/runtime_process.go - the pid-file/kill/orphan-sweep lifecycle to WRAP (repo rule: never hand-roll a second mechanism)
- internal/voice/AGENTS.md "Process lifecycle" - spawn/invariants contract
- .research/signal-telnyx-channels.md - json-rpc method names + notification payload shapes (authoritative for this leaf)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
package signal // internal/signal/rpc.go, supervisor.go, types.go

type CallState string // RINGING_INCOMING|RINGING_OUTGOING|CONNECTING|CONNECTED|RECONNECTING|ENDED

type CallEvent struct {
    EnvelopeSourceUUID string
    CallID             string
    State              CallState
    Reason             string // ENDED reason: busy|timeout|declined|hangup|...
}

type DataMessage struct {
    SourceUUID  string
    SourceName  string
    Body        string
    Attachments []string // local paths after signal-cli download
    Timestamp   int64
    GroupID     string   // empty = 1:1
}

type CallAudioMode struct {
    SocketAudio bool   // true = socket PCM mode (preferred)
    SocketPath  string // when SocketAudio
}

type Client struct{}
func Dial(socketPath string, log *slog.Logger) (*Client, error)
func (c *Client) Close() error
func (c *Client) StartCall(recipient string, mode CallAudioMode) (callID string, err error)
func (c *Client) AcceptCall(callID string) error
func (c *Client) HangupCall(callID string) error
func (c *Client) ListCalls() ([]CallInfo, error)
func (c *Client) SubscribeCallEvents(subscriptionID string) error
func (c *Client) Send(recipient, message string, attachments []string) (ts int64, err error)
func (c *Client) Typing(recipient string, stop bool) error
// Notification fan-out: RegisterCallEvents(fn func(CallEvent)), RegisterMessages(fn func(DataMessage)).

type Supervisor struct{}
func NewSupervisor(binary string, account string, socketPath string, cfg SupervisorConfig, log *slog.Logger) *Supervisor
// SupervisorConfig: idle stop, restart backoff. WRAPS llm.RuntimeProcess
// (pid file, kill, orphan sweep reuse - repo rule). Ensures socket exists
// before returning from Start (bounded wait), starts `jsonRpc --socket`.
func (s *Supervisor) Start(ctx context.Context) error
func (s *Supervisor) Stop(ctx context.Context) error
func (s *Supervisor) Client() (*Client, error) // dials socketPath
```

### What This Leaf Consumes

- llm.RuntimeProcess (child lifecycle). No voice/telnyx imports allowed in internal/signal (leg isolation; mirrors internal/voice/local.go:14-15 pin posture).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | types.go | Create: internal/signal/types.go | `go build ./internal/signal/` | package absent | all C3 types compile with json tags | Given CallEvent JSON `{"sourceUuid":"u","callId":"c","state":"CONNECTED"}`; When unmarshal; Then fields populated |
| T2 | rpc.go client | Create: internal/signal/rpc.go; Test: internal/signal/rpc_test.go (exists, RED) | `go test ./internal/signal/ -run Client` | Client undefined | unix-socket json-rpc 2.0: request-id mutex (concurrent requests serialize), response correlation, notification demux goroutine, Register fan-outs; stub test server exercises full method set | Given stub emitting callEvent CONNECTED then ENDED; When SubscribeCallEvents + StartCall; Then both events fire in order, correct callID |
| T3 | messaging methods | Modify: internal/signal/rpc.go (Send/Typing); Test: internal/signal/msg_test.go (exists, RED) | `go test ./internal/signal/ -run Msg` | methods absent | Send returns timestamp; envelope notification -> DataMessage with dedupe by (sourceUuid, timestamp); typing stop flag | Given two identical envelope notifications; When demux; Then handler fires once |
| T4 | supervisor.go | Create: internal/signal/supervisor.go; Test: internal/signal/supervisor_test.go (NEW - stub binary) | `go test ./internal/signal/ -run Supervisor` | Supervisor undefined | wraps RuntimeProcess; Start launches stub `signal-cli` (test fake script), waits for socket file (bounded), Client dials + completes one request; Stop kills process group; restart-on-crash with backoff capped | Given stub that crashes after first request; Then supervisor restarts within backoff and client recovers |
| T5 | config seam | Modify: internal/config/schema.go ([signal] section: enabled false, binary path, account, socket_path, idle_stop_seconds) + DefaultConfig + config template comments | `go build ./... && go test ./internal/config/` | section absent | defaults: enabled=false, binary "signal-cli", socket under $MEEPT_HOME/signal/signal.sock | Given DefaultConfig(); Then cfg.Signal.Enabled==false |

## Self-Verification Checklist

- [ ] All tasks implemented; gate tests passing (rpc_test, msg_test GREEN)
- [ ] Interface contracts satisfied exactly (C3 signatures)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] Do NOT COMMIT.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests passing, gate tests untouched
- [ ] Interface contracts match exactly (signatures, types, paths)
- [ ] No telnyx/voice imports in internal/signal; supervisor wraps RuntimeProcess (no hand-rolled lifecycle)
- [ ] Conventions followed; no bugs, no security issues (no account/credential logging)
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Credentials: signal-cli holds its own account state (linked device); the daemon NEVER handles Signal account credentials - the Supervisor only spawns the binary with the configured account handle. If a future change needs a secret (e.g. registration PIN), it MUST ride Broker.Value with the audit-script allowlist update.
- The stub binary for T4 is a tiny test-only script (shell or Go testdata) speaking minimal json-rpc over the socket - it must NOT be committed as a production artifact.
- Docs are incomplete upstream: if a payload shape from the research archive disagrees with a live stub's output, trust the archive AND flag the drift in your report for the orchestrator to reconcile.
