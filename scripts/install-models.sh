#!/usr/bin/env bash
#
# install-models.sh -- download the meept-designed model catalog.
#
# Models referenced by config/models.json5 live in one directory,
# $MEEPT_MODELS_DIR (default: ~/.meept/models). The daemon resolves the same
# variable via config expansion: models.json5 paths use
# ${MEEPT_MODELS_DIR:-$HOME/.meept/models}/... so a drive-mounted layout
# (e.g. MEEPT_MODELS_DIR=/Volumes/LLMs) keeps working unchanged.
#
# Subcommands:
#   install   prompt for the storage path + selection, then download (default)
#   status    report which catalog models are present/missing, no download
#   help
#
# Non-interactive overrides (all optional):
#   MEEPT_MODELS_DIR=/path        storage path (skips the prompt)
#   MEEPT_MODELS_SET=basic|full   model set (skips the prompt)
#                                 basic = 8B GGUF + 1.2B Extract GGUF
#                                 full  = basic + 8B MLX 4-bit
#   MEEPT_MODELS_SKIP=1           make targets no-op without prompting
#
# The "lfm-combined-sft" classifier entry (config/models.json5) is the user's
# own fine-tune with no public repo; it is reported as local-only and never
# downloaded.
#
# Downloads use the official LiquidAI Hugging Face repos:
#   LiquidAI/LFM2.5-8B-A1B-GGUF      (LFM2.5-8B-A1B-Q4_K_M.gguf,      ~5.2GB)
#   LiquidAI/LFM2-1.2B-Extract-GGUF  (LFM2-1.2B-Extract-Q4_K_M.gguf, ~0.8GB)
#   LiquidAI/LFM2.5-8B-A1B-MLX-4bit  (repo, ~4.5GB, Apple Silicon only)
#
# ASCII only. bash >= 3.2.
set -euo pipefail

MEEPT_HOME="${MEEPT_HOME:-$HOME/.meept}"
MEEPT_MODELS_DIR="${MEEPT_MODELS_DIR:-$MEEPT_HOME/models}"
BASE_URL="${MEEPT_HF_BASE_URL:-https://huggingface.co}"

# Catalog: name|repo|remote path|local path under MEEPT_MODELS_DIR|size|platform
CATALOG_8B_GGUF="lfm-8b-gguf|LiquidAI/LFM2.5-8B-A1B-GGUF|LFM2.5-8B-A1B-Q4_K_M.gguf|LiquidAI/LFM2.5-8B-A1B-GGUF/LFM2.5-8B-A1B-Q4_K_M.gguf|5.2GB|any"
CATALOG_EXTRACT="lfm2-extract|LiquidAI/LFM2-1.2B-Extract-GGUF|LFM2-1.2B-Extract-Q4_K_M.gguf|LiquidAI/LFM2-1.2B-Extract-GGUF/LFM2-1.2B-Extract-Q4_K_M.gguf|0.8GB|any"
CATALOG_8B_MLX="lfm-8b-mlx-4bit|LiquidAI/LFM2.5-8B-A1B-MLX-4bit||LiquidAI/LFM2.5-8B-A1B-MLX-4bit|4.5GB|metal"

say() { printf '%s\n' "$*"; }

have() { command -v "$1" >/dev/null 2>&1; }

# fetch URL DEST: streaming download to DEST, resumable (curl -C -).
fetch() {
	local dest="$2"
	mkdir -p "$(dirname "$dest")"
	if [ -s "$dest" ]; then
		say "  present: $dest"
		return 0
	fi
	if have curl; then
		curl -L --fail --retry 3 -C - -o "$dest.part" "$url" && mv "$dest.part" "$dest"
	elif have wget; then
		wget -c -O "$dest.part" "$url" && mv "$dest.part" "$dest"
	else
		say "  ERROR: neither curl nor wget found" >&2
		return 1
	fi
}

# fetch_repo REPO DEST_DIR: download a whole repo (MLX dirs are multi-file).
# Prefers `hf download` (resumable, deduped) over a tarball scrape.
fetch_repo() {
	local repo="$1" dest="$2"
	if [ -d "$dest" ] && [ -e "$dest/config.json" ]; then
		say "  present: $dest"
		return 0
	fi
	mkdir -p "$dest"
	if have hf; then
		hf download "$repo" --local-dir "$dest"
	elif have huggingface-cli; then
		huggingface-cli download "$repo" --local-dir "$dest"
	else
		say "  ERROR: 'hf' (or 'huggingface-cli') required for $repo" >&2
		say "  install with: pip install -U \"huggingface_hub[cli]\"" >&2
		return 1
	fi
}

# download ENTRY: ENTRY is name|repo|remote|local|size|platform.
download() {
	local entry="$1" name repo remote local size platform
	IFS='|' read -r name repo remote local size platform <<EOF2
$entry
EOF2
	# MLX models require Apple Silicon.
	if [ "$platform" = "metal" ]; then
		if [ "$(uname -s)" != "Darwin" ] || [ "$(uname -m)" != "arm64" ]; then
			say "  skipping $name (Apple Silicon only; not needed on this platform)"
			return 0
		fi
	fi
	local dest="$MEEPT_MODELS_DIR/$local"
	if [ -n "$remote" ]; then
		local url="$BASE_URL/$repo/resolve/main/$remote"
		say "  downloading $name ($size)..."
		fetch "$url" "$dest"
	else
		say "  downloading $name ($size, repo)..."
		fetch_repo "$repo" "$dest"
	fi
}

# collect_selection SET: echo the catalog entries for a set name.
collect_selection() {
	case "$1" in
	basic) say "$CATALOG_8B_GGUF"; say "$CATALOG_EXTRACT" ;;
	full) say "$CATALOG_8B_GGUF"; say "$CATALOG_EXTRACT"; say "$CATALOG_8B_MLX" ;;
	esac
}

status() {
	local missing=0
	say "models root: $MEEPT_MODELS_DIR"
	say ""
	local entry
	for entry in "$CATALOG_8B_GGUF" "$CATALOG_EXTRACT" "$CATALOG_8B_MLX"; do
		IFS='|' read -r name repo remote local size platform <<EOF2
$entry
EOF2
		local dest="$MEEPT_MODELS_DIR/$local" state
		if [ -n "$remote" ]; then
			if [ -s "$dest" ]; then state="present"; else state="MISSING"; fi
		else
			if [ -e "$dest/config.json" ]; then state="present"; else state="MISSING"; fi
		fi
		say "  $state  $name ($size)  $dest"
		[ "$state" = "MISSING" ] && missing=$((missing + 1))
	done
	say ""
	say "  local-only (no public repo, never downloaded):"
	say "    lfm-combined-sft  \$MEEPT_MODELS_DIR/lfm2.5-1.2b-combined-serialized-sft"
	if [ "$missing" -gt 0 ]; then
		say ""
		say "$missing model(s) missing. run: make deps-models"
		return 1
	fi
	say "all catalog models present."
}

prompt_and_install() {
	if [ -n "${MEEPT_MODELS_SKIP:-}" ]; then
		say "MEEPT_MODELS_SKIP=1: skipping model download"
		return 0
	fi

	# 1. Storage path (skipped when MEEPT_MODELS_DIR is exported).
	if [ -z "${MODELS_DIR_PROMPTED:-}" ] && [ -z "${MEEPT_MODELS_DIR_SET:-}" ]; then
		say "meept model storage"
		say ""
		printf 'storage path for model weights [%s]: ' "$MEEPT_MODELS_DIR"
		if [ -t 0 ]; then
			IFS= read -r answer || answer=""
			[ -n "$answer" ] && MEEPT_MODELS_DIR="$answer"
		else
			say "(non-tty: keeping default)"
		fi
	fi
	mkdir -p "$MEEPT_MODELS_DIR"

	# 2. Model selection.
	local set_name="${MEEPT_MODELS_SET:-}"
	if [ -z "$set_name" ] && [ -t 0 ]; then
		say ""
		say "download the meept-designed model catalog (LFM models)?"
		say "  1) basic  - 8B GGUF (chat, ~5.2GB) + 1.2B Extract GGUF (~0.8GB)   [any platform]"
		say "  2) full   - basic + 8B MLX 4-bit (~4.5GB, Apple Silicon only)"
		say "  3) none   - skip (models can be added later: make deps-models)"
		printf 'choice [1]: '
		IFS= read -r answer || answer="1"
		case "$answer" in
		2) set_name="full" ;;
		3) set_name="none" ;;
		*) set_name="basic" ;;
		esac
	elif [ -z "$set_name" ]; then
		set_name="basic"
	fi
	case "$set_name" in
	none)
		say "no models downloaded."
		return 0
		;;
	esac

	say ""
	say "downloading to $MEEPT_MODELS_DIR ..."
	local rc=0
	while IFS= read -r entry; do
		download "$entry" || rc=1
	done <<EOF3
$(collect_selection "$set_name")
EOF3
	if [ "$rc" -ne 0 ]; then
		say "" >&2
		say "one or more downloads failed; re-run 'make deps-models' to resume." >&2
		return 1
	fi
	say ""
	say "models installed. config references \${MEEPT_MODELS_DIR:-\$HOME/.meept/models};"
	say "if you chose a non-default path, export MEEPT_MODELS_DIR for the daemon"
	say "(e.g. in \$MEEPT_HOME/env or your shell profile)."
}

case "${1:-install}" in
install) prompt_and_install ;;
status) status ;;
help | -h | --help)
	say "usage: $0 [install|status]"
	say "  install   prompt for storage path + model set, download (default)"
	say "  status    report present/missing catalog models, no download"
	say "env: MEEPT_MODELS_DIR, MEEPT_MODELS_SET=basic|full, MEEPT_MODELS_SKIP=1"
	;;
*)
	say "unknown subcommand: $1 (use install|status)" >&2
	exit 2
	;;
esac
