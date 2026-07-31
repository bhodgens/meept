#!/usr/bin/env python3
"""Generate structured connectivity graphs for the meept codebase.

Extracts three layers of connectivity that are invisible to the compiler:
  1. Bus topic topology — publishers, subscribers, payload fields per topic
  2. RPC handler map — RegisterHandler topic → handler function
  3. HTTP route map — method + path → handler function
  4. WS event classification — bus topic → frontend event type

Outputs:
  docs/generated/bus-topology.json   — machine-readable
  docs/generated/bus-topology.md     — human-readable tables
  docs/generated/rpc-handlers.json
  docs/generated/http-routes.json
  docs/generated/ws-event-map.json

Run: python3 scripts/gen-connectivity-graph.py [--check]
  --check  Exit non-zero if generated files are stale (for CI/pre-commit)
"""

import json
import os
import re
import sys
from collections import defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
GENERATED_DIR = ROOT / "docs" / "generated"

# Directories to scan for Go source
SCAN_DIRS = ["internal", "cmd", "pkg"]
# Directories to exclude
EXCLUDE_DIRS = {"vendor", "testdata", "node_modules", ".git"}


def find_go_files():
    """Yield all .go file paths under SCAN_DIRS, excluding EXCLUDE_DIRS."""
    for scan_dir in SCAN_DIRS:
        base = ROOT / scan_dir
        if not base.exists():
            continue
        for dirpath, dirnames, filenames in os.walk(base):
            dirnames[:] = [d for d in dirnames if d not in EXCLUDE_DIRS]
            for f in filenames:
                if f.endswith(".go"):
                    yield Path(dirpath) / f


def rel(path: Path) -> str:
    """Return path relative to repo root."""
    try:
        return str(path.relative_to(ROOT))
    except ValueError:
        return str(path)


# ---------------------------------------------------------------------------
# 1. Bus topic topology
# ---------------------------------------------------------------------------

# Matches: bus.Publish("topic", msg) / s.bus.Publish("topic", msg) / e.bus.Publish(...)
RE_PUBLISH = re.compile(
    r'\.Publish\(\s*"([^"]+)"'
)

# Matches: bus.Subscribe(id, "topic") / s.bus.Subscribe("id", "topic")
# Also: bus.Subscribe("id", "topic.*") for wildcards
RE_SUBSCRIBE = re.compile(
    r'\.Subscribe\(\s*"[^"]*"\s*,\s*"([^"]+)"'
)

# Matches: bus.SubscribeWildcard(id, "prefix.*")
RE_SUBSCRIBE_WILDCARD = re.compile(
    r'\.SubscribeWildcard\(\s*"[^"]*"\s*,\s*"([^"]+)"'
)

# Payload field extraction: look for map[string]any{...} or payload["key"] = ...
# near a Publish call. We extract keys from the nearest map literal.
RE_PAYLOAD_KEY = re.compile(r'"([a-z_]+)"\s*:')


def extract_bus_topology():
    """Extract all bus.Publish and bus.Subscribe calls with file:line."""
    publishers = defaultdict(list)   # topic -> [{file, line, payload_keys}]
    subscribers = defaultdict(list)  # topic -> [{file, line, subscriber_id}]

    for fpath in find_go_files():
        try:
            lines = fpath.read_text(errors="replace").splitlines()
        except OSError:
            continue

        for i, line in enumerate(lines, 1):
            # Skip comments and test files for cleaner output
            stripped = line.strip()
            if stripped.startswith("//"):
                continue

            # Publishers
            for m in RE_PUBLISH.finditer(line):
                topic = m.group(1)
                # Try to extract payload keys from nearby lines (the map literal)
                payload_keys = _extract_payload_keys(lines, i - 1)
                publishers[topic].append({
                    "file": rel(fpath),
                    "line": i,
                    "payload_keys": sorted(payload_keys),
                })

            # Subscribers
            for m in RE_SUBSCRIBE.finditer(line):
                topic = m.group(1)
                # Extract subscriber ID (first string arg)
                sub_id_m = re.search(r'\.Subscribe\(\s*"([^"]*)"', line)
                sub_id = sub_id_m.group(1) if sub_id_m else "?"
                subscribers[topic].append({
                    "file": rel(fpath),
                    "line": i,
                    "subscriber_id": sub_id,
                })

            for m in RE_SUBSCRIBE_WILDCARD.finditer(line):
                topic = m.group(1)
                sub_id_m = re.search(r'\.SubscribeWildcard\(\s*"([^"]*)"', line)
                sub_id = sub_id_m.group(1) if sub_id_m else "?"
                subscribers[topic].append({
                    "file": rel(fpath),
                    "line": i,
                    "subscriber_id": sub_id,
                    "wildcard": True,
                })

    return publishers, subscribers


def _extract_payload_keys(lines, pub_line_idx):
    """Look backwards from a Publish call for a map[string]any literal and
    extract its keys. Scans up to 15 lines back."""
    keys = set()
    # Find the opening of the map literal
    for j in range(pub_line_idx, max(pub_line_idx - 15, -1), -1):
        line = lines[j]
        for m in RE_PAYLOAD_KEY.finditer(line):
            keys.add(m.group(1))
        if "map[string]any{" in line or "map[string]interface{}{" in line:
            break
    return keys


# ---------------------------------------------------------------------------
# 2. RPC handler map
# ---------------------------------------------------------------------------

RE_REGISTER_HANDLER = re.compile(
    r'RegisterHandler\(\s*"([^"]+)"\s*,\s*([^\)]+)'
)


def extract_rpc_handlers():
    """Extract all RegisterHandler("topic", handler) calls."""
    handlers = []
    for fpath in find_go_files():
        try:
            lines = fpath.read_text(errors="replace").splitlines()
        except OSError:
            continue
        for i, line in enumerate(lines, 1):
            if line.strip().startswith("//"):
                continue
            for m in RE_REGISTER_HANDLER.finditer(line):
                topic = m.group(1)
                handler = m.group(2).strip().rstrip(",")
                handlers.append({
                    "topic": topic,
                    "handler": handler,
                    "file": rel(fpath),
                    "line": i,
                })
    handlers.sort(key=lambda h: h["topic"])
    return handlers


# ---------------------------------------------------------------------------
# 3. HTTP route map
# ---------------------------------------------------------------------------

# Matches chi-style: r.Get("/path", handler) / r.Post("/path", handler)
# Also: r.HandleFunc("/path", handler) / mux.Handle("/path", handler)
RE_HTTP_ROUTE = re.compile(
    r'\.(Get|Post|Put|Patch|Delete|HandleFunc|Handle|Method|MethodFunc)\(\s*'
    r'(?:"([^"]+)"|`([^`]+)`)'
    r'(?:\s*,\s*([^\)]+))?'
)


def extract_http_routes():
    """Extract HTTP route registrations."""
    routes = []
    for fpath in find_go_files():
        try:
            lines = fpath.read_text(errors="replace").splitlines()
        except OSError:
            continue
        for i, line in enumerate(lines, 1):
            if line.strip().startswith("//"):
                continue
            for m in RE_HTTP_ROUTE.finditer(line):
                method = m.group(1)
                path = m.group(2) or m.group(3) or "?"
                handler = (m.group(4) or "").strip().rstrip(",")
                # Normalize method names
                method_map = {
                    "Get": "GET", "Post": "POST", "Put": "PUT",
                    "Patch": "PATCH", "Delete": "DELETE",
                    "HandleFunc": "ANY", "Handle": "ANY",
                    "Method": "CUSTOM", "MethodFunc": "CUSTOM",
                }
                routes.append({
                    "method": method_map.get(method, method),
                    "path": path,
                    "handler": handler,
                    "file": rel(fpath),
                    "line": i,
                })
    routes.sort(key=lambda r: (r["path"], r["method"]))
    return routes


# ---------------------------------------------------------------------------
# 4. WS event classification map
# ---------------------------------------------------------------------------

def extract_ws_event_map():
    """Parse transformBusEventToWS to extract topic → event type mapping."""
    server_file = ROOT / "internal" / "comm" / "http" / "server.go"
    if not server_file.exists():
        return []

    text = server_file.read_text(errors="replace")
    # Find the transformBusEventToWS function
    func_match = re.search(
        r'func transformBusEventToWS\(.*?\n\}',
        text, re.DOTALL
    )
    if not func_match:
        return []

    func_body = func_match.group(0)
    mappings = []

    # Extract case patterns and their eventType assignments
    # Pattern: case <condition>: ... eventType = "value"
    case_blocks = re.split(r'\bcase\b', func_body)
    for block in case_blocks[1:]:  # skip the part before first case
        # Find the eventType assignment
        et_match = re.search(r'eventType\s*=\s*"([^"]+)"', block)
        if not et_match:
            continue
        event_type = et_match.group(1)

        # Extract topic conditions from the case line
        case_line = block.split("\n")[0]
        # Match: topic == "x" || topic == "y"
        topics = re.findall(r'topic\s*==\s*"([^"]+)"', case_line)
        # Match: strings.HasPrefix(topic, "x.")
        prefixes = re.findall(r'strings\.HasPrefix\(topic,\s*"([^"]+)"\)', case_line)

        for t in topics:
            mappings.append({"topic_pattern": t, "match": "exact", "event_type": event_type})
        for p in prefixes:
            mappings.append({"topic_pattern": p + "*", "match": "prefix", "event_type": event_type})

    # Add the default case
    default_match = re.search(r'default:\s*\n\s*.*?eventType\s*=\s*"([^"]+)"', func_body, re.DOTALL)
    if default_match:
        mappings.append({"topic_pattern": "*", "match": "default", "event_type": default_match.group(1)})

    return mappings


# ---------------------------------------------------------------------------
# Cross-reference and report
# ---------------------------------------------------------------------------

def cross_reference(publishers, subscribers):
    """Find topics that are published but never subscribed, and vice versa."""
    pub_topics = set(publishers.keys())
    sub_topics = set(subscribers.keys())

    # Expand wildcard subscriptions
    expanded_subs = set()
    for t in sub_topics:
        if t.endswith(".*") or t.endswith(".#"):
            prefix = t.rsplit(".", 1)[0]
            # Match any topic starting with this prefix
            for pt in pub_topics:
                if pt.startswith(prefix):
                    expanded_subs.add(pt)
        else:
            expanded_subs.add(t)

    orphan_publishers = pub_topics - expanded_subs
    orphan_subscribers = sub_topics - pub_topics

    return {
        "published_not_subscribed": sorted(orphan_publishers),
        "subscribed_not_published": sorted(orphan_subscribers),
    }


def generate_markdown(publishers, subscribers, rpc_handlers, http_routes, ws_map, xref):
    """Generate a human-readable markdown report."""
    lines = []
    lines.append("# Meept Connectivity Graph")
    lines.append("")
    lines.append(f"Auto-generated by `scripts/gen-connectivity-graph.py`. Do not edit.")
    lines.append("")

    # --- Bus topology ---
    lines.append("## Bus Topic Topology")
    lines.append("")
    lines.append("| Topic | Publishers | Subscribers | Payload Keys |")
    lines.append("|-------|-----------|-------------|-------------|")
    all_topics = sorted(set(list(publishers.keys()) + list(subscribers.keys())))
    for topic in all_topics:
        pubs = publishers.get(topic, [])
        subs = subscribers.get(topic, [])
        pub_locs = ", ".join(f"`{p['file']}:{p['line']}`" for p in pubs) or "—"
        sub_locs = ", ".join(f"`{s['file']}:{s['line']}` ({s['subscriber_id']})" for s in subs) or "—"
        # Merge payload keys from all publishers
        keys = set()
        for p in pubs:
            keys.update(p.get("payload_keys", []))
        keys_str = ", ".join(sorted(keys)) if keys else "—"
        lines.append(f"| `{topic}` | {pub_locs} | {sub_locs} | {keys_str} |")
    lines.append("")

    # --- Orphan analysis ---
    lines.append("## Orphan Analysis")
    lines.append("")
    if xref["published_not_subscribed"]:
        lines.append("### Published but never subscribed (potential dead events)")
        lines.append("")
        for t in xref["published_not_subscribed"]:
            lines.append(f"- `{t}`")
        lines.append("")
    if xref["subscribed_not_published"]:
        lines.append("### Subscribed but never published (potential dead listeners)")
        lines.append("")
        for t in xref["subscribed_not_published"]:
            lines.append(f"- `{t}`")
        lines.append("")
    if not xref["published_not_subscribed"] and not xref["subscribed_not_published"]:
        lines.append("No orphans detected. All published topics have subscribers and vice versa.")
        lines.append("")

    # --- WS event map ---
    lines.append("## WS Event Classification")
    lines.append("")
    lines.append("| Bus Topic Pattern | Match | Frontend Event Type |")
    lines.append("|-------------------|-------|-------------------|")
    for m in ws_map:
        lines.append(f"| `{m['topic_pattern']}` | {m['match']} | `{m['event_type']}` |")
    lines.append("")

    # --- RPC handlers ---
    lines.append("## RPC Handlers")
    lines.append("")
    lines.append("| Topic | Handler | Location |")
    lines.append("|-------|---------|----------|")
    for h in rpc_handlers:
        lines.append(f"| `{h['topic']}` | `{h['handler']}` | `{h['file']}:{h['line']}` |")
    lines.append("")

    # --- HTTP routes ---
    lines.append("## HTTP Routes")
    lines.append("")
    lines.append("| Method | Path | Handler | Location |")
    lines.append("|--------|------|---------|----------|")
    for r in http_routes:
        lines.append(f"| {r['method']} | `{r['path']}` | `{r['handler']}` | `{r['file']}:{r['line']}` |")
    lines.append("")

    return "\n".join(lines)


def main():
    check_mode = "--check" in sys.argv

    print("Scanning Go source files...")
    publishers, subscribers = extract_bus_topology()
    rpc_handlers = extract_rpc_handlers()
    http_routes = extract_http_routes()
    ws_map = extract_ws_event_map()
    xref = cross_reference(publishers, subscribers)

    print(f"  Bus topics: {len(set(list(publishers.keys()) + list(subscribers.keys())))} "
          f"({len(publishers)} published, {len(subscribers)} subscribed)")
    print(f"  RPC handlers: {len(rpc_handlers)}")
    print(f"  HTTP routes: {len(http_routes)}")
    print(f"  WS event mappings: {len(ws_map)}")

    if xref["published_not_subscribed"]:
        print(f"  ⚠ {len(xref['published_not_subscribed'])} topics published but never subscribed")
    if xref["subscribed_not_published"]:
        print(f"  ⚠ {len(xref['subscribed_not_published'])} topics subscribed but never published")

    # Build JSON outputs
    bus_json = {
        "publishers": {t: pubs for t, pubs in sorted(publishers.items())},
        "subscribers": {t: subs for t, subs in sorted(subscribers.items())},
        "orphans": xref,
    }

    GENERATED_DIR.mkdir(parents=True, exist_ok=True)

    outputs = {
        "bus-topology.json": bus_json,
        "rpc-handlers.json": rpc_handlers,
        "http-routes.json": http_routes,
        "ws-event-map.json": ws_map,
    }

    md_content = generate_markdown(publishers, subscribers, rpc_handlers, http_routes, ws_map, xref)

    if check_mode:
        # Verify generated files are up to date
        stale = []
        for name, data in outputs.items():
            path = GENERATED_DIR / name
            if not path.exists():
                stale.append(name)
                continue
            existing = json.loads(path.read_text())
            if existing != data:
                stale.append(name)

        md_path = GENERATED_DIR / "bus-topology.md"
        if not md_path.exists() or md_path.read_text() != md_content:
            stale.append("bus-topology.md")

        if stale:
            print(f"\n❌ Stale generated files: {', '.join(stale)}")
            print("   Run: python3 scripts/gen-connectivity-graph.py")
            sys.exit(1)
        else:
            print("\n✅ All generated files are up to date.")
            sys.exit(0)

    # Write outputs
    for name, data in outputs.items():
        path = GENERATED_DIR / name
        path.write_text(json.dumps(data, indent=2) + "\n")
        print(f"  Wrote {rel(path)}")

    md_path = GENERATED_DIR / "bus-topology.md"
    md_path.write_text(md_content)
    print(f"  Wrote {rel(md_path)}")

    print("\nDone.")


if __name__ == "__main__":
    main()
