#!/usr/bin/env python3
"""Sync shipped config assets (skills, agents, prompts) into the meept home.

Target directory: $MEEPT_HOME (default ~/.meept) — the same resolution the
meept binaries use, so the sync always writes where the daemon reads.

Merge semantics — never clobbers user modifications:

For every file shipped in config/{skills,agents,prompts}:
  - not installed yet            -> install verbatim, record checksum
  - unchanged since last install -> fast-forward to the new version
  - user-modified + repo changed -> COLLISION: prompt the user (interactive)
                                    or apply --drift policy (see below)
  - shipped file removed upstream -> left on disk, reported

Collision policy (a file differs from last-shipped AND from current repo):
  interactive (default, when stdin is a tty):
      [k]eep mine  [t]ake new  [d]iff  [s]kip all collisions
  non-interactive (--drift or piped):
      keep   (default) — user file untouched, new default saved as <f>.new
      take              — repo version replaces the local file
                        (the local version is preserved as <f>.bak first)

A manifest at <target>/.install-manifest.json records the checksum of each
file as installed, so "user modified" is detected against the LAST SHIPPED
version, not the current one. This is what makes repeat installs safe.

--drift-test runs the drift test: simulates a repo change against the live
meept home in a sandbox and verifies the merge matrix, printing PASS/FAIL.
"""

import argparse
import difflib
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ASSET_DIRS = ["skills", "agents", "prompts"]
MANIFEST_NAME = ".install-manifest.json"


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            h.update(chunk)
    return h.hexdigest()


def load_manifest(target_root: Path) -> dict:
    mf = target_root / MANIFEST_NAME
    if mf.exists():
        try:
            return json.loads(mf.read_text())
        except (json.JSONDecodeError, OSError):
            return {}  # corrupt manifest: treat all as user-modified (safe)
    return {}


def save_manifest(target_root: Path, manifest: dict) -> None:
    mf = target_root / MANIFEST_NAME
    tmp = mf.with_suffix(".tmp")
    tmp.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    tmp.replace(mf)


def resolve_target() -> Path:
    """Mirror the binaries' resolution: $MEEPT_HOME else ~/.meept."""
    env = os.environ.get("MEEPT_HOME", "").strip()
    if env:
        p = Path(os.path.expanduser(env))
    else:
        p = Path.home() / ".meept"
    p.mkdir(parents=True, exist_ok=True)
    return p


def show_diff(installed: Path, incoming: Path) -> None:
    diff = difflib.unified_diff(
        installed.read_text(errors="replace").splitlines(keepends=True),
        incoming.read_text(errors="replace").splitlines(keepends=True),
        fromfile=f"yours ({installed})",
        tofile=f"new default ({incoming})",
    )
    sys.stdout.writelines(diff)


def prompt_collision(rel: str, installed: Path, incoming: Path,
                     state: dict) -> str:
    """Interactive collision prompt. Returns 'keep' or 'take'."""
    if state.get("skip_all"):
        return "keep"
    while True:
        print(f"\nCOLLISION: {rel}")
        print("  you modified this file AND the shipped default changed")
        choice = input(
            "  [k]eep mine / [t]ake new / [d]iff / [s]kip all: "
        ).strip().lower()
        if choice == "d":
            show_diff(installed, incoming)
            continue
        if choice == "s":
            state["skip_all"] = True
            return "keep"
        if choice == "k":
            return "keep"
        if choice == "t":
            return "take"
        print("  answer k, t, d, or s")


def sync_tree(asset: str, src_root: Path, dst_root: Path, manifest: dict,
              report: dict, args, state: dict) -> None:
    if not src_root.is_dir():
        return
    section = manifest.setdefault(asset, {})
    shipped_now = {}

    for src in sorted(src_root.rglob("*")):
        if not src.is_file():
            continue
        rel = src.relative_to(src_root).as_posix()
        shipped_now[rel] = sha256(src)
        dst = dst_root / rel

        if not dst.exists():
            dst.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(src, dst)
            report["installed"].append(f"{asset}/{rel}")
            continue

        current_sum = sha256(dst)
        new_sum = shipped_now[rel]
        was_shipped = section.get(rel)

        if current_sum == new_sum:
            continue  # identical already
        if current_sum != was_shipped and was_shipped is not None:
            # genuine collision: user changed it AND repo changed it
            if args.drift == "take":
                backup = dst.with_suffix(dst.suffix + ".bak")
                shutil.copy2(dst, backup)
                shutil.copy2(src, dst)
                report["took"].append(f"{asset}/{rel} (old saved as .bak)")
            elif args.drift == "keep" or not sys.stdin.isatty():
                new_file = dst.with_name(dst.name + ".new")
                if not new_file.exists() or sha256(new_file) != new_sum:
                    shutil.copy2(src, new_file)
                report["conflict"].append(
                    f"{asset}/{rel} (kept yours; new default at .new)")
            else:
                choice = prompt_collision(f"{asset}/{rel}", dst, src, state)
                if choice == "take":
                    backup = dst.with_suffix(dst.suffix + ".bak")
                    shutil.copy2(dst, backup)
                    shutil.copy2(src, dst)
                    report["took"].append(
                        f"{asset}/{rel} (old saved as .bak)")
                else:
                    new_file = dst.with_name(dst.name + ".new")
                    if not new_file.exists() or sha256(new_file) != new_sum:
                        shutil.copy2(src, new_file)
                    report["conflict"].append(
                        f"{asset}/{rel} (kept yours; new default at .new)")
        elif was_shipped is None:
            # installed before this manifest existed (pre-manifest drift):
            # can't prove who changed it -> conservative, treat as collision
            if args.drift == "take":
                backup = dst.with_suffix(dst.suffix + ".bak")
                shutil.copy2(dst, backup)
                shutil.copy2(src, dst)
                report["took"].append(
                    f"{asset}/{rel} (pre-manifest; old saved as .bak)")
            else:
                new_file = dst.with_name(dst.name + ".new")
                if not new_file.exists() or sha256(new_file) != new_sum:
                    shutil.copy2(src, new_file)
                report["premanifest"].append(
                    f"{asset}/{rel} (unknown provenance; kept yours, "
                    "new default at .new)")
        else:
            # unchanged since last ship: fast-forward
            shutil.copy2(src, dst)
            report["updated"].append(f"{asset}/{rel}")

    for rel in list(section.keys()):
        if rel not in shipped_now:
            report["removed_upstream"].append(f"{asset}/{rel} (left on disk)")
    section.clear()
    section.update(shipped_now)


def print_report(report: dict) -> None:
    if not any(report.values()):
        print("config sync: everything current")
        return
    print("config sync:")
    icons = {"installed": "+", "updated": "~", "took": "t",
             "conflict": "!", "premanifest": "?", "removed_upstream": "-"}
    for kind in ("installed", "updated", "took", "conflict", "premanifest",
                 "removed_upstream"):
        for item in sorted(report[kind]):
            print(f"  {icons[kind]} {item}")
    n_new = len(report["conflict"]) + len(report["premanifest"])
    if n_new:
        print(f"  {n_new} file(s) kept yours; review the .new files")


def drift_test(script: Path, repo_config: Path) -> int:
    """Simulate a full drift lifecycle against a sandbox meept home."""
    failures = []

    def check(name: str, cond: bool):
        print(f"  [{'PASS' if cond else 'FAIL'}] {name}")
        if not cond:
            failures.append(name)

    with tempfile.TemporaryDirectory(prefix="meept-drift-") as tmp:
        tmp = Path(tmp)
        home = tmp / "home"
        home.mkdir()
        repo = tmp / "repo" / "config"
        (repo / "skills" / "demo").mkdir(parents=True)
        env = {**os.environ, "MEEPT_HOME": str(home)}

        def run(*extra):
            return subprocess.run(
                [sys.executable, str(script), str(repo), str(home), *extra],
                capture_output=True, text=True, env=env)

        # v1 install
        (repo / "skills/demo/SKILL.md").write_text("demo v1\n")
        (repo / "agents/alpha/AGENT.md").parent.mkdir(parents=True)
        (repo / "agents/alpha/AGENT.md").write_text("alpha v1\n")
        r = run()
        check("v1 installs", (home / "skills/demo/SKILL.md").exists()
              and (home / "agents/alpha/AGENT.md").exists())

        # idempotent re-run
        r = run()
        check("re-run is a no-op", "everything current" in r.stdout)

        # fast-forward unmodified
        (repo / "skills/demo/SKILL.md").write_text("demo v2\n")
        run()
        check("unmodified fast-forwards",
              (home / "skills/demo/SKILL.md").read_text() == "demo v2\n")

        # collision: user edit + repo change, policy keep
        (home / "skills/demo/SKILL.md").write_text("demo v2 + my tweak\n")
        (repo / "skills/demo/SKILL.md").write_text("demo v3\n")
        run("--drift", "keep")
        check("collision keep preserves user file",
              (home / "skills/demo/SKILL.md").read_text()
              == "demo v2 + my tweak\n")
        check("collision keep writes .new",
              (home / "skills/demo/SKILL.md.new").read_text() == "demo v3\n")

        # collision, policy take
        run("--drift", "take")
        check("collision take installs v3",
              (home / "skills/demo/SKILL.md").read_text() == "demo v3\n")
        bak = home / "skills/demo/SKILL.md.bak"
        check("collision take backs up user file",
              bak.exists() and "my tweak" in bak.read_text())

        # MEEPT_HOME respected (sandbox wrote under override, not ~/.meept)
        check("MEEPT_HOME honored (nothing touched real home)",
              not (Path.home() / ".meept/skills/demo").exists())

        # upstream delete
        shutil.rmtree(repo / "agents/alpha")
        r = run()
        check("upstream delete left on disk + reported",
              (home / "agents/alpha/AGENT.md").exists()
              and "left on disk" in r.stdout)

        # key drift: a shipped key absent from the installed file is reported,
        # and the note reaches the sync output (this is what would have caught
        # a missing extract_model on an existing install).
        (repo / "models.json5").write_text('{\n  "model": "a",\n  "newkey": 1\n}\n')
        (home / "models.json5").write_text('{\n  "model": "a"\n}\n')
        check("key drift reports the absent shipped key",
              missing_top_level_keys(repo / "models.json5",
                                     home / "models.json5") == ["newkey"])
        check("key drift is empty when nothing is absent",
              missing_top_level_keys(repo / "models.json5",
                                     repo / "models.json5") == [])
        r = run()
        check("key drift note reaches the sync output",
              "models.json5: newkey" in r.stdout)

    print()
    if failures:
        print(f"DRIFT TEST: {len(failures)} FAILURE(S): {failures}")
        return 1
    print("DRIFT TEST: all cases pass")
    return 0


def missing_top_level_keys(template: Path, installed: Path) -> list:
    """Top-level json5 keys the template declares that the installed file lacks.

    Deliberately a line scan, not a json5 parse: it needs no dependency, and it
    catches the drift that actually hurts - a NEW shipped key an existing
    install never received. models.json5 gained extract_model and a whole
    local-extract provider, and no path propagated them (setup copies only when
    the file is absent; install overwrites wholesale), so on an install that
    predated the key json_extract failed with "extraction model not
    configured" and the tool was dead in production while correct in the repo.
    """
    def keys(path: Path) -> list:
        found = []
        if not path.is_file():
            return found
        for line in path.read_text(errors="replace").splitlines():
            if not line.startswith('  "') or line.startswith('   '):
                continue
            key, _, rest = line[3:].partition('"')
            if rest.startswith(":"):
                found.append(key)
        return found

    have = set(keys(installed))
    return [k for k in keys(template) if k not in have]


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("repo_config", nargs="?",
                    help="repo config dir (config/); optional with "
                         "--drift-test")
    ap.add_argument("meept_home", nargs="?",
                    help="target meept home (default: $MEEPT_HOME or "
                         "~/.meept)")
    ap.add_argument("--drift", choices=["keep", "take"], default="keep",
                    help="collision policy for non-interactive runs "
                         "(default: keep)")
    ap.add_argument("--drift-test", action="store_true",
                    help="run the self-verifying drift test and exit")
    args = ap.parse_args()

    script = Path(__file__).resolve()

    if args.drift_test:
        return drift_test(script, Path(__file__).parent.parent / "config")

    repo_cfg = Path(args.repo_config).resolve()
    if not repo_cfg.is_dir():
        print(f"error: {repo_cfg} is not a directory", file=sys.stderr)
        return 2

    if args.meept_home:
        target = Path(os.path.expanduser(args.meept_home))
        target.mkdir(parents=True, exist_ok=True)
    else:
        target = resolve_target()

    manifest = load_manifest(target)
    report = {k: [] for k in ("installed", "updated", "took", "conflict",
                              "premanifest", "removed_upstream")}
    state: dict = {}

    for asset in ASSET_DIRS:
        sync_tree(asset, repo_cfg / asset, target / asset,
                  manifest, report, args, state)

    save_manifest(target, manifest)
    print(f"target: {target}")
    print_report(report)

    # Flat config files (models.json5, meept.json5) are NOT in ASSET_DIRS: setup
    # copies one only when it is absent and install overwrites it wholesale, so
    # a key added to the shipped template never reaches an existing install.
    # Report that drift here instead of leaving it silent.
    drift = {}
    for name in ("models.json5", "meept.json5"):
        missing = missing_top_level_keys(repo_cfg / name, target / name)
        if missing:
            drift[name] = missing
    if drift:
        print()
        print("NOTE: the shipped config declares keys your file does not have.")
        print("Absent keys normally fall back to their defaults, which is fine for")
        print("a deliberately minimal file - but a key whose value exists ONLY in")
        print("the template (extract_model and the local-extract provider were")
        print("exactly that) silently disables a feature. Review these:")
        for name, keys in sorted(drift.items()):
            shown = keys[:8]
            more = len(keys) - len(shown)
            tail = f" (+{more} more)" if more > 0 else ""
            print(f"  {name}: {', '.join(shown)}{tail}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
