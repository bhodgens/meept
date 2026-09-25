# Installation

Meept is built from source. You need Go 1.22+ and an LLM provider.

There are four install paths. Pick one:

| Path | Command | Installs | Needs Flutter |
|------|---------|----------|---------------|
| Headless (recommended first) | `make install-cli` | binaries + config | no |
| Full desktop | `make install` | binaries + config + GUI + menubar | yes |
| Desktop apps only | `make install-desktop` | GUI + menubar apps | yes |
| Packaging payload | `make install-package` | binaries + web bundle, staged | yes |

Every install path builds first, then copies. Nothing is installed to
surprise locations, and every destination can be overridden.

## Prerequisites

- **Go 1.22+** — [Install Go](https://go.dev/doc/install)
- **make** and **python3** (config bootstrap and scripts)
- **An LLM provider** — at least one of:
  - A local runtime: llama.cpp (installed by `make deps-llama`) and/or
    mlx-lm (Apple Silicon, installed by `make deps-mlx`), with model weights
    (installed by `make deps-models`)
  - [Ollama](https://ollama.ai) (local, free, no API key needed)
  - OpenAI, Anthropic, or any OpenAI-compatible API (requires API key)

Flutter is needed only for the desktop GUI paths.

## Headless Install (CLI + daemon)

```bash
git clone https://github.com/caimlas/meept.git
cd meept
make deps            # go modules + llama.cpp build-floor check
make install-cli     # build, then install binaries + config
```

Binaries land in `~/go/bin` (the Go bin directory — add it to `PATH` if it
is not already). Config lands in `~/.meept/`. Then:

```bash
meept doctor         # report-only health + models check
meept-daemon -f      # start the daemon
```

## Full Desktop Install (macOS/Linux GUI)

```bash
make deps
make install
```

This is the full chain: llama.cpp floor check, config bootstrap, dev-key
provisioning, then binaries, the Flutter GUI app, and the menubar app.
It requires a working Flutter SDK. Use `make install-cli` on machines
without Flutter.

## Install Destinations (GNU-style variables)

All destinations are make variables. Override them on the command line:

```bash
make install-cli PREFIX=/opt/meept          # -> /opt/meept/bin
make install-cli BINDIR=$HOME/.local/bin    # or set the bin dir directly
```

| Variable | Default | Meaning |
|----------|---------|---------|
| `PREFIX` | *(empty)* | Optional installation prefix. When set, `BINDIR` becomes `$(PREFIX)/bin` and `WEBDIR` becomes `$(PREFIX)/share/meept/web`. |
| `BINDIR` | `$(go env GOPATH)/bin` (or `$(PREFIX)/bin`) | Where the `meept`, `meept-daemon`, `meept-lite` binaries go. |
| `APPSDIR` | `~/Applications` | Where the macOS `.app` bundles go. **Never derived from `PREFIX`**: macOS Finder/Spotlight do not index apps in arbitrary prefix trees, so a prefixed bundle would not launch. Set it explicitly if you want bundles elsewhere. |
| `WEBDIR` | `$MEEPT_HOME/webui` (or `$(PREFIX)/share/meept/web`) | Where `install-web` stages the Flutter web bundle. |
| `DESTDIR` | *(empty)* | Staging root for packaging. Every install write goes to `$(DESTDIR)$(BINDIR)` etc. Empty means write destinations directly. |
| `MEEPT_HOME` | `~/.meept` | Per-machine runtime home (config, databases, deps, models). Never staged under `DESTDIR`. |

`DESTDIR` is the standard two-stage install variable (the same role as in
GNU autotools or Debian packaging): build the payload tree without touching
the live system, then copy it into place.

### Packaging

```bash
make install-package DESTDIR=/tmp/pkg PREFIX=/usr/local
```

Stages binaries into `/tmp/pkg/usr/local/bin` and the Flutter web bundle
into `/tmp/pkg/usr/local/share/meept/web`. Config and the per-install API
key are deliberately excluded — they are per-machine runtime data. A
packaged GUI pairs with its daemon on first run (see
[flutter_gui.md](../workflows/flutter_gui.md)).

## Models and Local Runtimes

meept's default model catalog is the LiquidAI LFM family, served by
llama.cpp (any platform) or mlx-lm (Apple Silicon).

```bash
make deps-llama         # meept-scoped llama.cpp (>= b9660 tool-call floor)
make deps-models        # download model weights (interactive)
make deps-mlx           # mlx-lm into $MEEPT_DEPS/mlx-venv (Apple Silicon only)
```

`make deps-models` prompts for two things:

1. **Storage path** for the weights — default `$MEEPT_HOME/models`
   (`~/.meept/models`). Override non-interactively with
   `MEEPT_MODELS_DIR=/path`.
2. **Model set**:
   - `basic` — 8B chat GGUF (~5.2GB) + 1.2B Extract GGUF (~0.8GB); any
     platform
   - `full` — basic + the 8B MLX 4-bit (~4.5GB); the MLX entry is skipped
     automatically on non-Apple-Silicon machines, so non-Metal gets the GGUFs
   - `none` — skip; run `make deps-models` any time later

Downloads are resumable and idempotent (already-present weights are kept).
Check state at any time:

```bash
make deps-models-status     # or: meept doctor
```

### Model storage path wiring

`config/models.json5` references weights as
`${MEEPT_MODELS_DIR:-~/.meept/models}/...`. The daemon expands the variable
from the process environment, then from the executable resolver script
`~/.meept/env`. To keep weights on a mounted drive instead:

```bash
# in ~/.meept/env (or your shell profile):
MEEPT_MODELS_DIR=/Volumes/LLMs
```

No config edit needed. `meept doctor` reports each configured model as
present or missing (report-only; missing weights are a warning, never a
failure).

`make deps-models` is optional. A cloud-only machine skips it (choice
`none` or `MEEPT_MODELS_SKIP=1`) and configures a cloud provider instead.

## Build Without Installing

```bash
make build              # everything: daemon + CLI + gendoc + GUI + lite
go build -o bin/meept-daemon ./cmd/meept-daemon   # or individually
```

Binaries are placed in `bin/`:

| Binary | Description |
|--------|-------------|
| `bin/meept-daemon` | The background agent platform |
| `bin/meept` | The CLI client |
| `bin/meept-lite` | Minimalistic TUI client |

`bin/` builds never touch `BINDIR`, `APPSDIR`, or `MEEPT_HOME`.

## Initial Setup and Provider Configuration

`make install-cli` and `make install` bootstrap `~/.meept/` with the
shipped config templates (copy-if-absent; user edits are never clobbered).
For a from-scratch `bin/`-only workflow:

```bash
make setup
cp config/models.json5 ~/.meept/models.json5
```

Edit `~/.meept/models.json5` to add your API keys. For a local Ollama
setup, no API key is needed:

```json5
{
  "model": "ollama/llama3.2",
  "small_model": "ollama/llama3.2",
  "providers": {
    "ollama": {
      "api": "openai",
      "options": {
        "baseURL": "http://localhost:11434/v1"
      },
      "models": {
        "llama3.2": {
          "capabilities": ["code", "tool_use", "reasoning"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 128000
        }
      }
    }
  }
}
```

For a cloud provider like OpenRouter:

```json5
{
  "model": "openrouter/claude-sonnet",
  "providers": {
    "openrouter": {
      "api": "openai",
      "options": {
        "baseURL": "https://openrouter.ai/api/v1",
        "apiKey": "${OPENROUTER_API_KEY}"
      },
      "models": {
        "claude-sonnet": {
          "name": "anthropic/claude-3-sonnet",
          "capabilities": ["code", "reasoning", "tool_use"],
          "input_cost": 3.0,
          "output_cost": 15.0,
          "context_limit": 200000
        }
      }
    }
  }
}
```

## Verify Installation

```bash
meept doctor    # report-only: pidfile, socket, config, disk, models, orphans
```

## Uninstall

```bash
make uninstall          # stop + remove the service (config preserved)
make uninstall-all      # also remove binaries and $MEEPT_HOME (config + data)
```

`uninstall-all` removes exactly `$MEEPT_HOME` — honor any `MEEPT_HOME`
override you installed with.

## Optional: Build the Flutter GUI

Meept includes a cross-platform GUI built with Flutter. The make targets
handle the details; raw `flutter` commands are for development only.

### Prerequisites

- [Flutter SDK 3.0+](https://docs.flutter.dev/get-started/install)
- Platform-specific tools:
  - **macOS**: Xcode command line tools
  - **Linux**: GTK development libraries (`libgtk-3-dev`)
  - **Windows**: Visual Studio Build Tools

### Build targets

```bash
make build-gui          # dev build: embeds this machine's dev key + endpoint
make build-gui-dist     # distribution build: NO embedded key (first-run pairing)
```

The dev build is for local workflows (`make devbuild` embeds the same key).
Distribution builds must use `build-gui-dist`: a packaged GUI that shipped
the builder's key would let every recipient authenticate as the builder.
A key-less GUI pairs with the daemon on first launch (one-time code printed
by the daemon, exchanged over loopback only) — see
[flutter_gui.md](../workflows/flutter_gui.md).

### Run in development

```bash
cd ui/flutter_ui
flutter run                      # dev mode
flutter build macos              # macOS release
flutter build linux              # Linux release
flutter build windows            # Windows release
```

See [Quick Start](quick-start.md#optional-using-the-flutter-gui) for more details.
