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
                if f.endswith(".go") and not f.endswith("_test.go"):
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

# Matches wrapper-helper bodies: .Publish(topicVar, msg) where the topic is an
# identifier, not a string literal. Marks the enclosing function as a
# publish-through helper ONLY when the published identifier is one of the
# helper's own parameters (its topic flows from callers). Functions that
# publish a fixed identifier — a package-level const (selfimprove statusTopic)
# or a fixed literal like ChatHandler.sendResponse's "chat.response" — always
# emit the same topic regardless of arguments, so their parameter values are
# NOT topics and must not be attributed.
RE_PUBLISH_VAR = re.compile(
    r'\.Publish\(\s*([A-Za-z_][A-Za-z0-9_.]*)\s*,'
)

# A Publish whose first arg is a CONCATENATED literal ("push." + sessID,
# "task-completed-"+id) yields per-message dynamic topics. Record the constant
# prefix so the report can group them, and never treat the bare prefix as a
# complete concrete topic.
RE_PUBLISH_CONCAT = re.compile(
    r'\.Publish\(\s*"([^"]*)"\s*\+'
)

# Matches: bus.Subscribe(id, "topic")  /  bus.Subscribe(idConst, TopicConst)
# The topic arg is either a quoted string or a bare identifier (resolved via
# the const table). Identifier matches that turn out to be a word fragment of
# a Go func literal ("func(ctx ..." -> "fun"/"func") are rejected AFTER the
# match in extract code by requiring the char right after the ident to be ')'
# or ',' + space (a real call continues with another arg) — not a '('.
_TOPIC_ARG = r'(?:"([^"]+)"|([A-Za-z_][A-Za-z0-9_.]*))\b'
RE_SUBSCRIBE = re.compile(
    r'\.Subscribe(?:Wildcard)?\(\s*(?:"[^"]*"|[A-Za-z_][A-Za-z0-9_.]*)\s*,\s*' + _TOPIC_ARG
)

# Back-compat wildcard pattern (RE_SUBSCRIBE above covers both shapes; this
# one additionally tags the entry as a wildcard).
RE_SUBSCRIBE_WILDCARD = re.compile(
    r'\.SubscribeWildcard\(\s*(?:"[^"]*"|[A-Za-z_][A-Za-z0-9_.]*)\s*,\s*' + _TOPIC_ARG
)

# Matches direct publishes whose topic arg is a Go constant identifier:
#   bus.Publish(TopicPairStart, msg)
#   msgBus.Publish(agent.TopicTeamStart, busMsg)
RE_PUBLISH_CONST = re.compile(
    r'\.Publish\(\s*([A-Za-z_][A-Za-z0-9_.]*)\s*(?:,|\))'
)

# Payload field extraction: look for map[string]any{...} or payload["key"] = ...
# near a Publish call. We extract keys from the nearest map literal.
RE_PAYLOAD_KEY = re.compile(r'"([a-z_]+)"\s*:')

# MessageCallback table loops:  topics := map[string]bus.MessageCallback{
#   "task.create": h.handleTaskCreate, ... } followed by
#   for topic, callback := range topics { h.handler.Subscribe(topic, callback) }.
# Each map key whose value names a handler method is a live subscription. We
# detect the table literal keys and attribute the subscription to the loop's
# Subscribe line.
RE_HANDLER_TABLE = re.compile(
    r'^\s*(\w+)\s*:?=\s*map\[string\](?:[\w.*]+MessageCallback|func\([^)]*\)[^{]*)\s*\{'
)
RE_TABLE_KEY = re.compile(r'"([^"]+)"\s*:')

# Go constant declarations assigned a string literal:
#   TopicPairResult    = "pair.result"
#   const Foo = "bar"  /  Foo string = "bar"
RE_CONST_DECL = re.compile(
    r'^\s*(?:const\s+)?([A-Za-z_][A-Za-z0-9_]*)\s+(?:[A-Za-z_][A-Za-z0-9_\[\]*.]+\s+)?=\s*"([^"]+)"'
)


def resolve_identifier(name, const_table):
    """Resolve a Go identifier to its string-constant value when known.
    Unresolved identifiers pass through unchanged (they will then look like
    ordinary topic names and are visible in the report as unresolved)."""
    return const_table.get(name, name)


def extract_bus_topology():
    """Extract all bus.Publish and bus.Subscribe calls with file:line.

    Publisher detection covers two shapes:
      1. Direct: bus.Publish("topic", msg)
      2. Indirect via a wrapper helper: e.g. func (q *Q) publishEvent(topic
         string, ...) { ... bus.Publish(topic, msg) }. The helper's body
         contains .Publish(<identifier>); we find call sites of that helper
         (by name, string literal first arg) and attribute the topic there.
    """
    publishers = defaultdict(list)   # topic -> [{file, line, payload_keys}]
    subscribers = defaultdict(list)  # topic -> [{file, line, subscriber_id}]

    go_files = list(find_go_files())

    # Pass 0: build the package-level string-constant table so identifier args
    # (TopicPairResult etc.) can be resolved to their literal values.
    const_table = {}
    for fpath in go_files:
        try:
            lines = fpath.read_text(errors="replace").splitlines()
        except OSError:
            continue
        for line in lines:
            m = RE_CONST_DECL.match(line)
            if m:
                const_table[m.group(1)] = m.group(2)

    # Pass 1: collect direct publishes and wrapper-publish helpers.
    # publish_helpers maps helperName -> [(file, line)] of its Publish var site.
    publish_helpers = defaultdict(list)

    for fpath in go_files:
        try:
            lines = fpath.read_text(errors="replace").splitlines()
        except OSError:
            continue

        for i, line in enumerate(lines, 1):
            stripped = line.strip()
            if stripped.startswith("//"):
                continue

            for m in RE_PUBLISH.finditer(line):
                topic = m.group(1)
                payload_keys = _extract_payload_keys(lines, i - 1)
                publishers[topic].append({
                    "file": rel(fpath),
                    "line": i,
                    "payload_keys": sorted(payload_keys),
                })

            # Concatenated-literal publishes ("push." + sessID) produce a
            # dynamic family of topics. Record the constant prefix as an
            # explicit dynamic publisher instead of a fake concrete topic.
            for m in RE_PUBLISH_CONCAT.finditer(line):
                publishers[m.group(1) + "*"].append({
                    "file": rel(fpath),
                    "line": i,
                    "payload_keys": [],
                    "dynamic_prefix": True,
                })

            for m in RE_PUBLISH_CONST.finditer(line):
                const_name = m.group(1).split(".")[-1]
                if const_name in const_table:
                    topic = const_table[const_name]
                    payload_keys = _extract_payload_keys(lines, i - 1)
                    publishers[topic].append({
                        "file": rel(fpath),
                        "line": i,
                        "payload_keys": sorted(payload_keys),
                        "via_const": True,
                    })

            # Wrapper-helper bodies end in .Publish(topicVar, msg). Record the
            # enclosing function's name only when the published identifier is
            # one of that function's own parameters — otherwise the function
            # publishes a fixed topic and its parameter values are not topics.
            for m in RE_PUBLISH_VAR.finditer(line):
                var = m.group(1).split(".")[0]  # unwrap h.topicField style too
                fn_idx = _enclosing_func_idx(lines, i - 1)
                if fn_idx is None:
                    continue
                header = lines[fn_idx]
                params = _func_params(header)
                if var in params:
                    fn = _enclosing_func(lines, i - 1)
                    if fn:
                        publish_helpers[fn].append(rel(fpath))

    # Pass 2: attribute topics published through helpers. A call like
    # q.publishEvent("topic", map...) whose helper publishes a variable topic
    # is treated as a real publisher of that topic.
    re_call = None  # built lazily per helper name
    helper_callsites = []
    seen_names = set(publish_helpers)
    if seen_names:
        names = "|".join(re.escape(n) for n in sorted(seen_names))
        # Helper call sites pass the topic as either a string literal or a Go
        # constant identifier (often as the 2nd positional arg after a
        # sessionID): q.publishEvent("topic", ...) and
        # d.publishEvent(sess.ID, TopicCollabResult, ...). Scan ALL args: any
        # quoted string or known const identifier supplies the topic; the
        # const table disambiguates identifiers from session IDs.
        re_call = re.compile(
            r'\.(?:' + names + r')\(([^;\n]*)'
        )

    for fpath in go_files:
        if not re_call:
            break
        try:
            lines = fpath.read_text(errors="replace").splitlines()
        except OSError:
            continue
        for i, line in enumerate(lines, 1):
            stripped = line.strip()
            if stripped.startswith("//") or not re_call:
                continue
            for m in re_call.finditer(line):
                args = m.group(1)
                topic = None
                lit = re.search(r'"([^"]+)"', args)
                # A constant identifier is preferred over a bare identifier;
                # strings like sess.ID stay unresolved so they are skipped.
                for tok in args.replace("(", " ").split(","):
                    tok = tok.strip().rstrip(",").strip()
                    base = tok.split(".")[-1]
                    if base in const_table:
                        topic = const_table[base]
                        break
                if topic is None and lit:
                    topic = lit.group(1)
                if topic is None:
                    continue
                payload_keys = _extract_payload_keys(lines, i - 1)
                publishers[topic].append({
                    "file": rel(fpath),
                    "line": i,
                    "payload_keys": sorted(payload_keys),
                    "via_helper": True,
                })
                helper_callsites.append((rel(fpath), i))

    # Subscribers (same file walk as before). Topic and subscriber-id args may
    # be string literals OR Go constants (h.bus.Subscribe(src, TopicPairResult));
    # unresolved identifiers are resolved through the const table built above.
    for fpath in go_files:
        try:
            lines = fpath.read_text(errors="replace").splitlines()
        except OSError:
            continue

        for i, line in enumerate(lines, 1):
            stripped = line.strip()
            if stripped.startswith("//"):
                continue

            # MessageCallback table keys feed `for topic := range topics`
            # loops whose body calls handler.Subscribe(topic, callback). Every
            # key in such a table is a real subscription; attribute it to the
            # enclosing loop's Subscribe site when one follows.
            table_m = RE_HANDLER_TABLE.match(line)
            if table_m:
                table_name = table_m.group(1)
                j = i  # 1-based
                depth = line.count("{") - line.count("}")
                while j < len(lines) and depth > 0:
                    tline = lines[j]
                    if not tline.strip().startswith("//"):
                        for km in RE_TABLE_KEY.finditer(tline):
                            subscribers[km.group(1)].append({
                                "file": rel(fpath),
                                "line": i,
                                "subscriber_id": f"{table_name}:{km.group(1)}",
                                "via_handler_table": True,
                            })
                    depth += tline.count("{") - tline.count("}")
                    if depth <= 0:
                        break
                    j += 1

            for regex, wild in ((RE_SUBSCRIBE, False), (RE_SUBSCRIBE_WILDCARD, True)):
                for m in regex.finditer(line):
                    literal, ident = m.group(1), m.group(2)
                    if literal is not None:
                        topic = literal
                    else:
                        # Reject func-literal callbacks: Subscribe("t", func(
                        # captures "func" as an identifier topic. Detect by
                        # checking whether a '(' directly follows the ident.
                        after = line[m.end(2):]
                        if ident is not None and after.startswith("("):
                            continue
                        # Loop-var subscribes (for t, c := range tbl {
                        # Subscribe(t, c) }) are covered by the table path.
                        if ident in ("topic", "callback") and "range topics" in "".join(
                            lines[max(i - 30, 0):i - 1]
                        ):
                            continue
                        topic = resolve_identifier(ident, const_table)
                    # Subscriber-id group only matches a quoted arg; constant
                    # ids are resolved the same way when possible.
                    id_m = re.search(
                        r'\.Subscribe(?:Wildcard)?\(\s*"([^"]*)"\s*,', line
                    )
                    if id_m:
                        sub_id = id_m.group(1)
                    else:
                        alt_m = re.search(r'\.Subscribe(?:Wildcard)?\(\s*([A-Za-z_][A-Za-z0-9_.]*)\s*,', line)
                        sub_id = resolve_identifier(alt_m.group(1), const_table) if alt_m else "?"
                    entry = {
                        "file": rel(fpath),
                        "line": i,
                        "subscriber_id": sub_id,
                    }
                    if wild:
                        entry["wildcard"] = True
                    subscribers[topic].append(entry)

    return publishers, subscribers


def _func_params(header):
    """Extract parameter names from a Go func signature line.

    Handles `func (r *T) Name(a string, b int)` and plain funcs. The argument
    list is the LAST parenthesized group on the header line (the receiver
    comes first in methods), so take the segment between the final '(' and
    its matching ')'. Nested generic types can confuse naive first-match
    regexes; rfind avoids them for every signature shape in this codebase.
    """
    header = header.rstrip()
    if not header.endswith("{"):
        return set()
    close = header.rfind(")")
    open_p = header.rfind("(", 0, close)
    if close == -1 or open_p == -1:
        return set()
    inner = header[open_p + 1:close]
    names = set()
    for part in inner.split(","):
        part = part.strip()
        if not part:
            continue
        # "name type", "name, type collapsed by split", or bare "name"
        fname = part.split()[0]
        if re.match(r'^[A-Za-z_][A-Za-z0-9_]*$', fname):
            names.add(fname)
    return names


def _enclosing_func_idx(lines, idx):
    """Walk backwards from a 0-based line index to the line holding the
    enclosing function's `func ...(` declaration. Returns the index or None."""
    for j in range(idx, max(idx - 400, -1), -1):
        if re.match(r'^func\b', lines[j].strip()):
            return j
    return None


def _enclosing_func(lines, idx):
    """Walk backwards from a 0-based line index to find the enclosing Go
    function's declared name, tolerating receiver methods."""
    fn_re = re.compile(r'^func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)')
    for j in range(idx, max(idx - 400, -1), -1):
        line = lines[j]
        m = fn_re.match(line.strip())
        if m and "func" in line:
            return m.group(1)
    return None


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
    """Extract all RegisterHandler("topic", handler) calls.

    makeProxy registrations (RegisterHandler("m", p.makeProxy("requestTopic",
    "responseTopic", ...))) bridge RPC methods onto the bus: the RPC method
    publishes requestTopic. Record that edge so proxied topics are not
    misreported as publisher-less.
    """
    handlers = []
    proxy_edges = []
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
                # p.makeProxy("queue.enqueue", "queue.result", 10*time.Second)
                pm = re.search(
                    r'makeProxy\(\s*"([^"]+)"\s*,\s*"([^"]+)"', handler
                )
                if not pm:
                    pm = re.search(r'makeProxy\b', line) and re.search(
                        r'\bmakeProxy\(\s*"([^"]+)"\s*,\s*"([^"]+)"', line
                    )
                if pm:
                    proxy_edges.append({
                        "method": topic,
                        "request_topic": pm.group(1),
                        "response_topic": pm.group(2),
                        "file": rel(fpath),
                        "line": i,
                        "via_rpc_proxy": True,
                    })
    handlers.sort(key=lambda h: h["topic"])
    return handlers, proxy_edges


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
    matched_wildcards = set()
    for t in sub_topics:
        if t.endswith(".*") or t.endswith(".#"):
            prefix = t.rsplit(".", 1)[0]
            # Match any topic starting with this prefix
            for pt in pub_topics:
                if pt.startswith(prefix):
                    expanded_subs.add(pt)
                    matched_wildcards.add(t)
        else:
            expanded_subs.add(t)

    # Topics whose subscribers or publishers are runtime-dynamic values
    # (per-request IDs, per-connection subscriber names, unresolved local
    # variables). These are not real topic names and cannot be cross-referenced
    # statically; suppress them from the orphan lists instead of misreporting
    # them as dead listeners.
    runtime_dynamic = {
        t for t in (pub_topics | sub_topics)
        if not re.match(r'^[a-z][a-z0-9_.]*$', t)   # snake_case literals only
        or re.match(r'^(reply|response|combined|tui)\.', t)
        or t in ("replyTopic", "responseTopic", "combinedTopic", "topic", "callback")
        or "*" in t and not t.endswith(".*") and not t.endswith(".#")
    }

    orphan_publishers = pub_topics - expanded_subs - runtime_dynamic
    # A wildcard subscription is satisfied when at least one concrete topic
    # matches its prefix; only unmatched wildcards are dead listeners.
    orphan_subscribers = {
        t for t in sub_topics
        if not (t in matched_wildcards or t in pub_topics)
        and t not in runtime_dynamic
    }

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
    rpc_handlers, proxy_edges = extract_rpc_handlers()
    http_routes = extract_http_routes()
    ws_map = extract_ws_event_map()

    # RPC-proxy bridging: every makeProxy method publishes its request topic.
    for edge in proxy_edges:
        publishers.setdefault(edge["request_topic"], []).append({
            "file": edge["file"],
            "line": edge["line"],
            "payload_keys": [],
            "via_rpc_proxy": True,
            "method": edge["method"],
        })

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
