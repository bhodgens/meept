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
    """Yield all .go file paths under SCAN_DIRS, excluding EXCLUDE_DIRS.

    Sorted output: os.walk order is filesystem-dependent (macOS and Linux
    differ), which made generated artifacts non-reproducible across OSes
    and false-stale in CI's freshness check.
    """
    found = []
    for scan_dir in SCAN_DIRS:
        base = ROOT / scan_dir
        if not base.exists():
            continue
        for dirpath, dirnames, filenames in os.walk(base):
            dirnames[:] = [d for d in dirnames if d not in EXCLUDE_DIRS]
            for f in filenames:
                if f.endswith(".go") and not f.endswith("_test.go"):
                    found.append(Path(dirpath) / f)
    yield from sorted(found)


def rel(path: Path) -> str:
    """Return path relative to repo root."""
    try:
        return str(path.relative_to(ROOT))
    except ValueError:
        return str(path)


# ---------------------------------------------------------------------------
# 1. Bus topic topology
# ---------------------------------------------------------------------------

# Matches: bus.Publish("topic", msg) / s.bus.Publish("topic", msg) /
# e.bus.Publish(...) — including PublishExternalOnly and PublishBlocking
# variants, which deliver to the same topic namespace.
RE_PUBLISH = re.compile(
    r'\.Publish(?:ExternalOnly|Blocking)?\(\s*"([^"]+)"'
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

# A seam forwarder: `h.publishEvent(topicVar, payload)` where the receiver
# carries a topic-passing FUNC FIELD rather than a bus. Identical pass-through
# shape to a .Publish( wrapper - the topic flows from the caller through the
# wired closure to the bus - but the field's name is per-type, so it cannot be
# a fixed literal. Built per receiver type from the seam index (see
# _seam_call_regexes), which is why it is not a module-level constant.
RE_SEAM_FIELD_DECL = re.compile(
    r'^\s*(?:(\w+)\s+)?(\*?[\w.]+(?:\[[^\]\s]*\])?)\s*(?:`.*)?$'
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
# The \b word-boundary AFTER the alternation fails on the quoted branch: a
# closing quote followed by a non-word char (e.g. `Subscribe("id", "topic")`
# -> `"topic")`) has no word boundary, so every quoted-topic Subscribe was
# silently unmatched. Use a conditional: require \b only for the ident branch
# (group 2); the quoted branch (group 1) ends at the closing quote.
_TOPIC_ARG = r'(?:"([^"]+)"|([A-Za-z_][A-Za-z0-9_.]*))(?(2)\b)'
# The subscribe-id argument may be an EXPRESSION (subID+"-chatprogress")
# or an identifier — match anything up to the first comma so the topic arg
# stays the strict part of the pattern.
_ANY_FIRST_ARG = r'(?:[^,()]|\([^)]*\))*'
RE_SUBSCRIBE = re.compile(
    r'\.Subscribe\(\s*' + _ANY_FIRST_ARG + r',\s*' + _TOPIC_ARG
)

# Back-compat wildcard pattern (this one additionally tags the entry as a
# wildcard; RE_SUBSCRIBE keeps covering the same call sites otherwise).
RE_SUBSCRIBE_WILDCARD = re.compile(
    r'\.SubscribeWildcard\(\s*' + _ANY_FIRST_ARG + r',\s*' + _TOPIC_ARG
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

# String-slice topic lists:  topics := []string{ "a", "b", ... }  consumed by
#   for _, topic := range topics { bus.Subscribe(expr, topic) }. Each literal in
# the slice is a live subscription. Detected like the MessageCallback tables.
RE_TOPIC_SLICE = re.compile(
    r'^\s*(\w+)\s*:?\s*=\s*\[\]string\{'
)

# Go constant declarations assigned a string literal:
#   TopicPairResult    = "pair.result"
#   const Foo = "bar"  /  Foo string = "bar"
RE_CONST_DECL = re.compile(
    r'^\s*(?:const\s+)?([A-Za-z_][A-Za-z0-9_]*)\s+(?:[A-Za-z_][A-Za-z0-9_\[\]*.]+\s+)?=\s*"([^"]+)"'
)


# ---------------------------------------------------------------------------
# Structural bus-reachability analysis (the anti-fabrication guard)
#
# A method is only a publish-through helper when it can actually REACH the bus.
# The original implementation matched helper call sites by bare method name
# (`re_call`), so ANY method named emit/publish/sendResponse became a bus
# publisher and its arguments were harvested as topics: internal/acp's
# Session.emit is a CHANNEL send (it never touches the bus - internal/acp does
# not even import internal/bus) yet it published six invented topics: closed,
# done, error, message_chunk, permission_request, tool_call.
#
# Three STRUCTURAL routes count as "this type can reach the bus". Every gate
# below is structural and type-based, never name-based:
#
#   1. receiver-holds-bus  - the receiver type has a field or embedded type
#      that IS a bus: *bus.MessageBus / bus.MessageBus, a same-package
#      *MessageBus (bus.SubscriptionHandler), or an interface that
#      bus.MessageBus satisfies (parkEventBus, handoffBus). Resolved
#      transitively, so a receiver holding a bus-holder struct
#      (taskCreatorAdapter.registry -> task.Registry) counts too.
#   2. helper-body-publishes - the helper's own body calls a bus publish method
#      on a receiver that resolves to a bus type.
#   3. wired-bus-seam      - the receiver type carries a topic-passing func
#      field (`publishEvent EventPublisher`, EventPublisher =
#      func(topic string, ...)) whose setter is wired at some call site with a
#      closure that really publishes to a bus. This keeps
#      internal/agent's VerificationAutoTrigger attributable for
#      agent.model_escalated even though it holds no bus itself: loop.go
#      wires SetEventPublisher with a closure over loop.bus.
#
# Method names only SELECT which call sites to examine; they never qualify one.

# Methods on MessageBus that deliver to the topic namespace.
BUS_PUBLISH_METHODS = ("Publish", "PublishBlocking", "PublishExternalOnly")
# Plus the typed helpers that deliver through those methods.
BUS_PUBLISH_FUNCS = ("PublishT", "PublishBlockingT")

RE_BUS_PUBLISH_CALL = re.compile(
    r'\.(?:' + "|".join(BUS_PUBLISH_METHODS + BUS_PUBLISH_FUNCS) + r')\s*\('
)

RE_TYPE_STRUCT = re.compile(r"^type\s+(\w+)\s*(?:\[[^\]]*\])?\s+struct\s*\{")
RE_TYPE_INTERFACE = re.compile(r"^type\s+(\w+)\s*interface\s*\{")
RE_TYPE_NAMED_FUNC = re.compile(r"^type\s+(\w+)\s+func\(\s*\w+\s+string\b")
# A bus-shaped interface method: Publish(topic string, msg *models.BusMessage) int
RE_BUS_IFACE_METHOD = re.compile(
    r'(?:' + "|".join(BUS_PUBLISH_METHODS) +
    r')\s*\(\s*\w+\s+string\s*,\s*\w+\s+\*?[\w.]*BusMessage\b'
)
# `name Type` / embedded `Type` / `a, b Type` struct field line.
RE_FIELD_DECL = re.compile(
    r"^\s*(?:(\w+)\s+)?(\*?[\w.]+(?:\[[^\]\s]*\])?)\s*(?:`.*)?$"
)
# Receiver-bearing method header: func (r *T) Name(...) {
RE_FUNC_RECEIVER = re.compile(
    r'^func\s+\(\s*(\w+)\s+(\*?)([\w.]+)\s*\)\s*([A-Za-z_]\w*)'
)
# Local binding of a variable to its type: `x := &T{`, `var x *T`,
# `x := NewT(`, `x := *T`.
RE_LOCAL_BIND = re.compile(
    r'^\s*(?:(\w+)\s*(?::=|=)\s*&?\s*(\w[\w.]*)\s*(?:\{|\()'
    r'|\bvar\s+(\w+)\s+(\*?[\w.]+))'
)
# bus_package == "" for files inside internal/bus itself.
BUS_IMPORT = "github.com/caimlas/meept/internal/bus"


def _base_type(typ):
    """Strip pointers, slices and package qualifiers: `*bus.MessageBus` ->
    `MessageBus`, `[]*Worker` -> `Worker`, `pkg.Handler` -> `Handler`.
    Returns None for non-identifier types (`[]string`, `map[string]any`,
    `error`) which cannot name a declaration.

    Unexported names are KEPT: `tinyBus`, `parkEventBus` and `handoffBus` are
    real struct field types in this codebase, and dropping them made every
    interface-typed bus holder look bus-less."""
    if not typ:
        return None
    t = typ.strip().lstrip("*")
    t = re.sub(r'^\[\]\*?', '', t)
    t = t.split("[")[0].strip()
    # Drop any package qualifier: bus.MessageBus -> MessageBus. Field types are
    # resolved within the declaring package, so the qualifier carries no
    # information for the index.
    t = t.rsplit(".", 1)[-1]
    if not re.match(r'^[A-Za-z_]\w*$', t):
        return None
    return t


def _pkg_dir(path):
    return path.parent


def _collect_block(lines, idx):
    """Return (body lines, index just past the block) for the brace-delimited
    construct OPENING on lines[idx]. When lines[idx] opens nothing (depth 0 on
    entry) the block is empty and scanning stops immediately - callers must
    anchor on a real `func ... {` header."""
    depth = lines[idx].count("{") - lines[idx].count("}")
    body = []
    j = idx + 1
    while depth > 0 and j < len(lines):
        depth += lines[j].count("{") - lines[j].count("}")
        if depth > 0:
            body.append(lines[j])
        j += 1
    return body, j


def _func_param_types(header):
    """Map parameter name -> declared type for a Go func header line
    (`func (r *T) Name(a string, b int) {`)."""
    header = header.rstrip()
    close = header.rfind(")")
    open_p = header.rfind("(", 0, close)
    if close == -1 or open_p == -1:
        return {}
    inner = header[open_p + 1:close]
    out = {}
    for part in _split_top_level(inner):
        toks = part.strip().split()
        if len(toks) >= 2:
            typ = toks[-1]
            for name in toks[:-1]:
                if re.match(r'^\w+$', name):
                    out[name] = typ
    return out


def _split_top_level(text):
    """Split on commas that are not nested inside brackets."""
    parts, depth, cur = [], 0, ""
    for ch in text:
        if ch in "([{":
            depth += 1
        elif ch in ")]}":
            depth -= 1
        if ch == "," and depth == 0:
            parts.append(cur)
            cur = ""
            continue
        cur += ch
    parts.append(cur)
    return parts


def _strip_strings_and_comments(text):
    """Blank out string literals and comments so brace counting and regex
    scans never trip over `{`/`}` or `.Publish(` inside a string."""
    out = []
    i, n = 0, len(text)
    while i < n:
        ch = text[i]
        if ch == '"':
            out.append(" ")
            i += 1
            while i < n and text[i] != '"':
                if text[i] == "\\":
                    i += 1
                i += 1
            i += 1
            continue
        if ch == "`":
            out.append(" ")
            i += 1
            while i < n and text[i] != "`":
                i += 1
            i += 1
            continue
        if ch == "/" and i + 1 < n and text[i + 1] == "/":
            while i < n and text[i] != "\n":
                out.append(" ")
                i += 1
            continue
        if ch == "/" and i + 1 < n and text[i + 1] == "*":
            while i + 1 < n and not (text[i] == "*" and text[i + 1] == "/"):
                out.append("\n" if text[i] == "\n" else " ")
                i += 1
            out.append("  ")
            i += 2
            continue
        out.append(ch)
        i += 1
    return "".join(out)


def _scan_index(files):
    """Build the package-qualified declaration index.

    Every key is (package directory, bare type name) so same-named types in
    different packages never collide.
    """
    decl = {}        # (pkg, name) -> {"kind", "fields", "types"}
    methods = defaultdict(list)   # (pkg, recvtype) -> [(method, header idx, lines)]
    topic_funcs = defaultdict(set)  # pkg -> {named func types taking topic string}
    bus_pkg_dirs = set()

    for fpath in files:
        pkg = _pkg_dir(fpath)
        raw = fpath.read_text(errors="replace").splitlines()
        code = [_strip_strings_and_comments(l) for l in raw]
        if any(BUS_IMPORT in l for l in raw):
            bus_pkg_dirs.add(pkg)
        i = 0
        while i < len(code):
            stripped = code[i].strip()
            if not stripped:
                i += 1
                continue
            sm = RE_TYPE_STRUCT.match(stripped)
            im = RE_TYPE_INTERFACE.match(stripped)
            nm = RE_TYPE_NAMED_FUNC.match(stripped)
            if sm or im:
                decl_re = sm or im
                name = decl_re.group(1)
                kind = "struct" if sm else "iface"
                body, i = _collect_block(code, i)
                entry = {"kind": kind, "fields": {}, "types": set()}
                if kind == "struct":
                    for bl in body:
                        b = bl.rstrip()
                        if not b.strip():
                            continue
                        fm = RE_FIELD_DECL.match(b)
                        if fm:
                            fname, ftype = fm.group(1), fm.group(2)
                            if fname:
                                entry["fields"][fname] = ftype
                        for tok in re.findall(r'\*?[\w.]+', b):
                            base = _base_type(tok)
                            if base:
                                entry["types"].add(base)
                else:
                    for bl in body:
                        if RE_BUS_IFACE_METHOD.search(bl):
                            entry["types"].add("MessageBus")
                            break
                decl[(pkg, name)] = entry
            elif nm:
                name = nm.group(1)
                topic_funcs[pkg].add(name)
                sig = stripped
                depth = sig.count("(") - sig.count(")")
                j = i
                while depth > 0 and j + 1 < len(code):
                    j += 1
                    sig += " " + code[j].strip()
                    depth += code[j].count("(") - code[j].count(")")
                decl[(pkg, name)] = {"kind": "func", "fields": {}, "types": set()}
                i = j
            else:
                hm = RE_FUNC_RECEIVER.match(stripped)
                if hm:
                    _, j = _collect_block(code, i)
                    methods[(pkg, _base_type(hm.group(3)))].append(
                        (hm.group(4), i, code)
                    )
                    i = j
                    continue
            i += 1

    # A file inside internal/bus declares the bus itself.
    for pkg in bus_pkg_dirs:
        decl[(pkg, "MessageBus")] = {
            "kind": "struct", "fields": {}, "types": {"MessageBus"},
        }

    # Transitively close over "holds a bus". A struct is a bus holder when one
    # of its field types is a bus holder in the SAME package (Go field types
    # are unqualified inside their package) or is a bus-shaped interface.
    changed = True
    while changed:
        changed = False
        for (pkg, name), entry in list(decl.items()):
            if entry["kind"] != "struct" or "MessageBus" in entry["types"]:
                continue
            for tok in list(entry["types"]):
                dep = decl.get((pkg, tok))
                if dep and "MessageBus" in dep["types"]:
                    entry["types"].add("MessageBus")
                    changed = True
                    break

    bus_names = {
        (pkg, name) for (pkg, name), entry in decl.items()
        if entry["kind"] in ("struct", "iface") and "MessageBus" in entry["types"]
    }

    # --- route 3: topic-passing func fields wired to a real publish -----
    # (recv pkg, recv type) -> [(field, setter name)]
    seam_candidates = {}
    for (pkg, recv_type), ms in methods.items():
        for name, hdr_idx, code in ms:
            if not name.startswith("Set"):
                continue
            params = _func_param_types(code[hdr_idx])
            recv_var = RE_FUNC_RECEIVER.match(
                code[hdr_idx].strip()).group(1)
            body, _ = _collect_block(code, hdr_idx)
            for bl in body:
                am = re.match(r'^\s*' + re.escape(recv_var) +
                              r'\.(\w+)\s*=\s*(\w+)\s*$', bl)
                if not am:
                    continue
                fname, pname = am.group(1), am.group(2)
                for cand in (params.get(pname), params.get(fname)):
                    if _base_type(cand) in topic_funcs.get(pkg, ()):
                        seam_candidates.setdefault(
                            (pkg, recv_type), set()).add((fname, name))

    # Which setters actually receive a closure that publishes to a bus?
    # Scoped per package: `SetEventPublisher` wired in internal/agent must not
    # make an unrelated package's setter of the same name count as wired.
    wired_setters = set()
    for fpath in files:
        pkg = _pkg_dir(fpath)
        raw = fpath.read_text(errors="replace").splitlines()
        code = [_strip_strings_and_comments(l) for l in raw]
        for i, line in enumerate(code, 1):
            if line.strip().startswith("//"):
                continue
            sm = re.search(r'\.(Set[A-Z]\w*)\(', line)
            if not sm:
                continue
            # The closure may start on THIS line (`.SetEventPublisher(func(...`
            # —) or a later one; scan forward until the call's own braces
            # balance, honouring the brace opened before the call.
            span = _closure_span(code, i - 1)
            if span is None:
                continue
            if RE_BUS_PUBLISH_CALL.search(span):
                wired_setters.add((pkg, sm.group(1)))

    seams = set()
    for key, cands in seam_candidates.items():
        for fname, setter in cands:
            if (key[0], setter) in wired_setters:
                seams.add((key[0], key[1], fname))

    return {
        "decl": decl,
        "methods": methods,
        "bus_names": bus_names,
        "seams": seams,
        "bus_pkg_dirs": bus_pkg_dirs,
    }


def _closure_span(code, idx, limit=15):
    """Return the source text of the func literal a `.Set*(...)` call starting
    on code[idx] passes, or None when that call passes no literal.

    Brace counting starts at zero and accumulates FROM this line, so a closure
    opening on the same line as the call is captured:
        trigger.SetEventPublisher(func(topic string, payload map[string]any) {
            ...
            bus.Publish(topic, msg)
        })
    Scanning stops once the literal's braces balance, or when the call closes
    without ever opening one (`h.SetOverrideApplier(loop.SetX)`).
    """
    depth = 0
    saw_func = False
    out = []
    j = idx
    last = min(len(code), idx + limit)
    while j < last:
        line = code[j]
        depth += line.count("{") - line.count("}")
        out.append(line)
        if "func(" in line:
            saw_func = True
        if depth > 0:
            saw_func = True        # inside a literal body
        if saw_func and depth <= 0:
            break
        if not saw_func and line.rstrip().endswith(")"):
            break                  # the call closed without a literal
        j += 1
    if not saw_func:
        return None
    return "\n".join(out)


def _bus_holds_bus(idx, pkg, type_name):
    """Structural predicate: can this receiver type reach the bus?"""
    if not type_name:
        return False
    key = (pkg, _base_type(type_name))
    if key in idx["bus_names"]:
        return True
    for spkg, styp, sfield in idx["seams"]:
        if spkg == pkg and styp == _base_type(type_name):
            return True
    return False


def _enclosing_func_decl_idx(lines, idx):
    """Walk backwards for the nearest TOP-LEVEL `func` declaration.

    Unlike _enclosing_func_idx (bounded to 400 lines because the callers that
    predate the structural guard wanted a cheap nearest-declaration guess),
    this is UNBOUNDED and exact: Go does not nest declarations, so the nearest
    preceding `^func` at column zero IS the enclosing declaration. The bound
    broke on large methods - internal/agent/tactical.go's OnJobCompleted is
    ~880 lines long, so every `ts.publishEvent(...)` inside it resolved to no
    receiver at all and the call-site gate dropped its topics.
    """
    for j in range(idx, -1, -1):
        if re.match(r'^func\b', lines[j]):
            return j
    return None


def _receiver_type(idx, lines, site_idx):
    """Resolve the receiver type of the function enclosing lines[site_idx]."""
    hdr = _enclosing_func_decl_idx(lines, site_idx)
    if hdr is None:
        return None
    hm = RE_FUNC_RECEIVER.match(lines[hdr].strip())
    return hm.group(3) if hm else None


def _resolve_local_type(lines, site_idx, ident):
    """Resolve a local variable / parameter / receiver to its Go type name,
    walking backwards from the call site to the enclosing func header."""
    hdr = _enclosing_func_decl_idx(lines, site_idx)
    stop = hdr if hdr is not None else max(site_idx - 400, -1)
    for j in range(site_idx, stop, -1):
        bm = RE_LOCAL_BIND.match(lines[j])
        if bm:
            name = bm.group(1) or bm.group(3)
            if name == ident:
                typ = bm.group(2) or bm.group(4)
                return _base_type(typ)
    if hdr is not None:
        params = _func_param_types(lines[hdr])
        if ident in params:
            return _base_type(params[ident])
        hm = RE_FUNC_RECEIVER.match(lines[hdr].strip())
        if hm and hm.group(1) == ident:
            return _base_type(hm.group(3))
    return None


def _resolve_recv_expr_type(idx, lines, site_idx, expr, pkg):
    """Resolve a call-site receiver expression (`h`, `a.registry`, `bus`) to
    its Go type name, walking struct fields through the package index."""
    parts = [p for p in expr.split(".") if p]
    if not parts:
        return None
    typ = _resolve_local_type(lines, site_idx, parts[0])
    if typ is None:
        typ = _receiver_type(idx, lines, site_idx)
    for part in parts[1:]:
        entry = idx["decl"].get((pkg, typ)) if pkg else None
        if entry is None:
            return None
        ftype = entry["fields"].get(part)
        if ftype is None:
            return None
        typ = _base_type(ftype)
    return typ


def _receiver_is_bus_publisher(idx, fpath, lines, site_idx, expr):
    """Call-site gate (Pass 2): the receiver expression must be able to reach
    the bus - structurally, never by method name."""
    pkg = _pkg_dir(fpath)
    typ = _resolve_recv_expr_type(idx, lines, site_idx, expr, pkg)
    return _bus_holds_bus(idx, pkg, typ)


def _seam_call_regexes(idx):
    """{package -> [(field name, compiled regexp)]} for every wired topic-func
    seam field. The regexp matches `.field(<identifier>,` so a forwarder whose
    topic argument is the enclosing function's own parameter can register as a
    publish-through helper."""
    by_pkg = defaultdict(list)
    for pkg, recv_type, field in idx["seams"]:
        rx = re.compile(r'\.' + re.escape(field) +
                        r'\(\s*([A-Za-z_][A-Za-z0-9_.]*)\s*,')
        by_pkg[pkg].append((field, rx))
    return by_pkg


def _helper_can_publish(idx, fpath, lines, publish_idx):
    """Registration gate (Pass 1). True when the enclosing method can reach
    the bus structurally:
      route 1 - its receiver type holds a bus (transitively);
      route 2 - its own body contains a bus publish call on a bus receiver;
      route 3 - its receiver type carries a wired bus-publish func field.
    """
    pkg = _pkg_dir(fpath)
    recv_type = _receiver_type(idx, lines, publish_idx)
    if recv_type and _bus_holds_bus(idx, pkg, recv_type):
        return True
    # route 2: the helper's own body publishes to a bus. Bound the scan to the
    # enclosing function's own braces so a Publish in a LATER function never
    # qualifies this one.
    hdr = _enclosing_func_decl_idx(lines, publish_idx)
    if hdr is not None:
        body, _end = _collect_block(lines, hdr)
        offset = hdr + 1
        for k, bl in enumerate(body):
            j = offset + k
            # Only the CURRENT function's publish site and what follows it:
            # the qualified call is the one we are gating.
            if j < publish_idx:
                continue
            if not RE_BUS_PUBLISH_CALL.search(bl):
                continue
            for cm in RE_BUS_PUBLISH_CALL.finditer(bl):
                recv = cm.string[:cm.start()].split()[-1].split("(")[-1]
                if not recv:
                    continue
                if _receiver_is_bus_publisher(idx, fpath, lines, j, recv):
                    return True
    return False



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
    # (TopicPairResult etc.) can be resolved to their literal values. The
    # per-directory variant scopes resolution to Go package semantics: an
    # unqualified identifier is only visible inside its own package.
    const_table = {}
    const_table_by_dir = {}
    for fpath in go_files:
        try:
            lines = fpath.read_text(errors="replace").splitlines()
        except OSError:
            continue
        for line in lines:
            m = RE_CONST_DECL.match(line)
            if m:
                const_table[m.group(1)] = m.group(2)
                const_table_by_dir.setdefault(fpath.parent, {})[m.group(1)] = m.group(2)

    # Pass 1: collect direct publishes and wrapper-publish helpers.
    # publish_helpers maps helperName -> [(file, line)] of its Publish var site.
    publish_helpers = defaultdict(list)

    # Structural bus-reachability index (anti-fabrication guard). Built once
    # and consulted at BOTH gates below.
    type_index = _scan_index(go_files)

    # Seam forwarders: for each (package, receiver type) whose wired
    # topic-func seam field is known, a `.field(topicVar, ...)` call is a
    # pass-through publish. Regexps are per-field because the field name is
    # per-type. Without this, a seam forwarder is invisible to Pass 1
    # (RE_PUBLISH_VAR only recognises the literal `.Publish(` shape) and its
    # topics would be lost.
    seam_re = _seam_call_regexes(type_index)

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
                # Skip identifiers that are parameters (or locals) of the
                # enclosing function — e.g. publishEvent's own `eventType`
                # parameter. RE_CONST_DECL can wrongly pick up assignments
                # to same-named variables elsewhere (e.g. a switch-case
                # `eventType = "event"`), which would fabricate a topic.
                fn_idx = _enclosing_func_idx(lines, i - 1)
                if fn_idx is not None and const_name in _func_params(lines[fn_idx]):
                    continue
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
            # publishes a fixed topic and its parameter values are not topics
            # — AND the function can actually reach the bus structurally
            # (_helper_can_publish). That second gate is what stops a
            # channel-send method named `emit` (internal/acp.Session.emit) from
            # registering as a publish-through helper and inventing topics.
            for m in RE_PUBLISH_VAR.finditer(line):
                var = m.group(1).split(".")[0]  # unwrap h.topicField style too
                fn_idx = _enclosing_func_idx(lines, i - 1)
                if fn_idx is None:
                    continue
                header = lines[fn_idx]
                params = _func_params(header)
                if var not in params:
                    continue
                if not _helper_can_publish(type_index, fpath, lines, i - 1):
                    continue
                fn = _enclosing_func(lines, i - 1)
                if fn:
                    publish_helpers[fn].append(rel(fpath))

    # Pass 2: attribute topics published through helpers. A call like
    # q.publishEvent("topic", map...) whose helper publishes a variable topic
    # is treated as a real publisher of that topic.
    re_call = None  # built lazily per helper name
    helper_callsites = []
    seen_names = set(publish_helpers)
    # Exclude the bare name Publish: BusService.Publish and friends call
    # s.bus.Publish(req.Topic, ...) with `req` a parameter, which registers
    # "Publish" itself as a wrapper — then re_call matches EVERY .Publish(
    # call in the repo and fabricates publishers via const-table accidents
    # (e.g. topic "event" from the eventType switch-assignment). A method
    # named Publish is the bus API, not a pass-through helper.
    seen_names -= {"Publish", "publish"}
    if seen_names:
        names = "|".join(re.escape(n) for n in sorted(seen_names))
        # Helper call sites pass the topic as either a string literal or a Go
        # constant identifier (often as the 2nd positional arg after a
        # sessionID): q.publishEvent("topic", ...) and
        # d.publishEvent(sess.ID, TopicCollabResult, ...). Scan ALL args: any
        # quoted string or known const identifier supplies the topic; the
        # const table disambiguates identifiers from session IDs.
        # The receiver expression is captured so the call-site structural gate
        # below can prove the receiver can reach the bus.
        re_call = re.compile(
            r'([A-Za-z_][\w.]*)\.(?:' + names + r')\(([^;\n]*)'
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
                recv_expr, args = m.group(1), m.group(2)
                # Call-site structural gate: the receiver of a helper call must
                # be able to reach the bus. This is the second half of the
                # anti-fabrication guard - Pass 1 proved the helper itself can
                # publish, this proves the CALLER's receiver can. A bare-name
                # match with no receiver check is what let internal/acp's
                # channel-sending Session.emit invent six topics; s.emit(...) on
                # a Session (no bus field, no bus publish in its body) now
                # fails here even though the name matches.
                if not _receiver_is_bus_publisher(
                    type_index, fpath, lines, i - 1, recv_expr
                ):
                    continue
                topic = None
                lit = re.search(r'"([^"]+)"', args)
                # A constant identifier is preferred over a bare identifier;
                # strings like sess.ID stay unresolved so they are skipped.
                # Go scoping: an UNQUALIFIED identifier can only name a
                # constant declared in the caller's own package, so the const
                # table is scoped to the call-site's package directory. A
                # qualified `pkg.Ident` still resolves against the global table.
                # Without this, `h.sendResponse(msg, ...)` in internal/agent
                # picked up the TUI's unrelated `const msg = "(no reason
                # given)"` and fabricated a bus topic out of a TUI string.
                pkg_consts = const_table_by_dir.get(fpath.parent, const_table)
                for tok in args.replace("(", " ").split(","):
                    tok = tok.strip().rstrip(",").strip()
                    if not tok:
                        continue
                    base = tok.split(".")[-1]
                    qualified = "." in tok
                    if base in (pkg_consts if not qualified else const_table):
                        table = pkg_consts if not qualified else const_table
                        topic = table[base]
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

            # String-slice topic list (e.g. internal/worker/pool.go): collect
            # the slice literals, then attribute them to a following
            # for-range loop whose Subscribe(topic, ...) uses the loop var.
            slice_m = RE_TOPIC_SLICE.match(line)
            if slice_m:
                slice_name = slice_m.group(1)
                j = i  # 1-based
                depth = line.count("{") - line.count("}")
                literals = []
                while j < len(lines) and (depth > 0 or j == i):
                    tline = lines[j]
                    if not tline.strip().startswith("//"):
                        for lit in re.findall(r'"([^"]+)"', tline):
                            literals.append(lit)
                    depth += tline.count("{") - tline.count("}")
                    if depth <= 0:
                        break
                    j += 1
                # Find the consuming range-loop Subscribe within 40 lines.
                for k in range(j, min(j + 40, len(lines))):
                    if f"range {slice_name}" not in lines[k]:
                        continue
                    for k2 in range(k, min(k + 6, len(lines))):
                        sm = RE_SUBSCRIBE.search(lines[k2])
                        if sm and sm.group(2) == "topic":
                            for lit in literals:
                                subscribers[lit].append({
                                    "file": rel(fpath),
                                    "line": k2 + 1,
                                    "subscriber_id": f"slice:{slice_name}:{lit}",
                                    "via_topic_slice": True,
                                })
                            break
                    break

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
                        # Expression ids (subID+"-chatprogress") have no plain
                        # identifier; fall back to the raw id text before the
                        # comma so the entry stays attributable.
                        expr_m = re.search(
                            r'\.Subscribe(?:Wildcard)?\(\s*([^,]{1,80}?),', line
                        )
                        sub_id = (
                            resolve_identifier(alt_m.group(1), const_table)
                            if (alt_m := re.search(r'\.Subscribe(?:Wildcard)?\(\s*([A-Za-z_][A-Za-z0-9_.]*)\s*,', line))
                            else (expr_m.group(1).strip() if expr_m else "?")
                        )
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

    # Topics with a documented external/dynamic publish or consume path that
    # static source scanning cannot see. Each entry: topic -> reason. These
    # are suppressed from BOTH orphan lists so they don't resurface as false
    # positives on every regeneration.
    annotated_orphans = {
        # External clients publish {"topic": "dispatcher.stats"} via the
        # "bus.publish" RPC; the responder lives at components.go
        # (dispatcher-stats-handler). The mirror "dispatcher.stats.result"
        # response topic is consumed by those same external clients.
        "dispatcher.stats": "external request endpoint via bus.publish RPC; responder in internal/daemon/components.go",
        "dispatcher.stats.result": "response for external dispatcher.stats requesters",
        # Forward-looking hook: Orchestrator subscribes; ContextFirewall does
        # not emit bus events yet (orchestrator.go handleContextCompressed).
        "llm.context_compressed": "forward-looking subscription; publisher (ContextFirewall) not yet implemented",
        # Agent loops publish run results on agent.result; the chat reply
        # path uses chat.response instead. The topic is an external/debug
        # tap (TUI event subscriptions), so no daemon subscriber is expected.
        "agent.result": "external/debug tap; chat replies flow via chat.response",
    }

    orphan_publishers = pub_topics - expanded_subs - runtime_dynamic - set(annotated_orphans)
    # A wildcard subscription is satisfied when at least one concrete topic
    # matches its prefix; only unmatched wildcards are dead listeners.
    orphan_subscribers = {
        t for t in sub_topics
        if not (t in matched_wildcards or t in pub_topics)
        and t not in runtime_dynamic
        and t not in annotated_orphans
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
        # The proxy consumes the response topic via a runtime Subscribe
        # (proxy.go makeProxy: Subscribe(msgID, responseTopic)) — record it
        # as a subscriber so response topics are not misreported as orphans.
        subscribers.setdefault(edge["response_topic"], []).append({
            "file": edge["file"],
            "line": edge["line"],
            "subscriber_id": f"rpc-proxy:{edge['method']}",
            "via_rpc_proxy": True,
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
            # First-difference debug (CI has no artifacts to inspect):
            # show a bounded unified diff for the first stale file so the
            # cross-platform divergence is diagnosable from the log alone.
            import difflib
            name = stale[0]
            path = GENERATED_DIR / name
            if name.endswith(".json") and path.exists():
                existing = json.loads(path.read_text())
                fresh = outputs[name]
                old_txt = json.dumps(existing, indent=2, sort_keys=True).splitlines()
                new_txt = json.dumps(fresh, indent=2, sort_keys=True).splitlines()
                diff = list(difflib.unified_diff(old_txt, new_txt,
                                                 "committed", "generated", lineterm=""))
                print(f"\n   first stale file: {name} (diff, first 60 lines):")
                for line in diff[:60]:
                    print("   " + line)
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


# ---------------------------------------------------------------------------
# Self-test: regression pins for the structural anti-fabrication guard
#
# Run: python3 scripts/gen-connectivity-graph.py --selftest
#
# These are non-vacuous by construction: every pin runs the SAME gate code the
# generator runs, against synthetic Go fixtures whose bus/non-bus status is
# decided by the fixture text, and asserts an outcome that a name-based match
# would get wrong. PIN 3 in particular fails if anyone reverts the guard to
# bare-name matching.
# ---------------------------------------------------------------------------

def _fixture(tmp: Path, name: str, body: str) -> Path:
    d = tmp / name
    d.mkdir(parents=True, exist_ok=True)
    (d / "fixture.go").write_text(body)
    return d / "fixture.go"


def _publishers_for(pkg_dir: Path):
    """Run the real Pass-1/Pass-2 gates over one synthetic package and return
    the topics it produces. Mirrors extract_bus_topology without touching the
    repo or writing any file."""
    go_files = [pkg_dir / "fixture.go"]
    idx = _scan_index(go_files)
    const_table = {}
    const_table_by_dir = {}
    lines = go_files[0].read_text().splitlines()
    for line in lines:
        cm = RE_CONST_DECL.match(line)
        if cm:
            const_table[cm.group(1)] = cm.group(2)
            const_table_by_dir.setdefault(go_files[0].parent, {})[cm.group(1)] = cm.group(2)

    publishers = defaultdict(list)
    publish_helpers = set()
    for i, line in enumerate(lines, 1):
        if line.strip().startswith("//"):
            continue
        for mm in RE_PUBLISH_VAR.finditer(line):
            var = mm.group(1).split(".")[0]
            hdr = _enclosing_func_idx(lines, i - 1)
            if hdr is None or var not in _func_params(lines[hdr]):
                continue
            if not _helper_can_publish(idx, go_files[0], lines, i - 1):
                continue
            fn = _enclosing_func(lines, i - 1)
            if fn:
                publish_helpers.add(fn)

    names = set(publish_helpers) - {"Publish", "publish"}
    if not names:
        return publishers, publish_helpers
    re_call = re.compile(
        r'([A-Za-z_][\w.]*)\.(?:' + "|".join(re.escape(n) for n in sorted(names)) +
        r')\(([^;\n]*)'
    )
    for i, line in enumerate(lines, 1):
        if line.strip().startswith("//"):
            continue
        for m in re_call.finditer(line):
            recv_expr, args = m.group(1), m.group(2)
            if not _receiver_is_bus_publisher(
                idx, go_files[0], lines, i - 1, recv_expr
            ):
                continue
            topic = None
            for tok in args.replace("(", " ").split(","):
                tok = tok.strip().rstrip(",").strip()
                if not tok:
                    continue
                base = tok.split(".")[-1]
                if base in const_table_by_dir.get(go_files[0].parent, {}):
                    topic = const_table_by_dir[go_files[0].parent][base]
                    break
            if topic is None:
                lit = re.search(r'"([^"]+)"', args)
                topic = lit.group(1) if lit else None
            if topic is not None:
                publishers[topic].append((i, recv_expr))
    return publishers, publish_helpers


# The internal/acp shape, verbatim in structure: a receiver whose ONLY
# outbound path is a Go channel, with a method named `emit`. internal/acp does
# not import internal/bus and contains zero .Publish( calls - this fixture
# reproduces that isolation, so nothing in it can reach a bus.
CHANNEL_EMITTER = '''package fixture

// Session holds NO bus - the channel is the whole point.
type Session struct {
	events chan SessionEvent
}

type SessionEvent struct {
	Kind string
}

func (s *Session) emit(ev SessionEvent) {
	select {
	case s.events <- ev:
	default:
	}
}

func (s *Session) notice(kind string) {
	s.emit(SessionEvent{Kind: kind})
}
'''

# Same channel emitter, but the package ALSO contains an unrelated `.Publish(`
# call elsewhere. This is the pattern-vacuity trap (PIN 3): the naive signal
# "this file calls .Publish( somewhere, so its `emit` must be a bus helper"
# is satisfied, yet Session still cannot reach the bus. The fixture therefore
# trips every name/param/sibling-based heuristic a reverted guard would use.
CHANNEL_EMITTER_WITH_SIBLING = '''package fixture

type Session struct {
	events chan SessionEvent
}

type SessionEvent struct {
	Kind string
}

func (s *Session) emit(ev SessionEvent) {
	select {
	case s.events <- ev:
	default:
	}
}

func (s *Session) notice(kind string) {
	s.emit(SessionEvent{Kind: kind})
}

// An unrelated sink elsewhere in the same file. It is a plain channel writer,
// NOT a bus publish - there is no bus in this package at all. The `.Publish(`
# on it is the vacuity bait: it satisfies the "this file publishes somewhere"
# heuristic that a name-based guard would use, while proving nothing about
// whether Session.emit can reach a bus.
func (s *Session) flush() {
	s.Publish(SessionEvent{Kind: "flush"})
}

// Publish is a channel writer with a bus-shaped NAME.
func (s *Session) Publish(ev SessionEvent) {
	select {
	case s.events <- ev:
	default:
	}
}
'''

# The internal/agent shape: the same method name on a receiver that holds a bus.
BUS_EMITTER = '''package fixture

import (
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// RoutingTelemetry holds a real bus.
type RoutingTelemetry struct {
	bus *bus.MessageBus
}

func (rt *RoutingTelemetry) emit(topic string, data map[string]any) {
	msg, _ := models.NewBusMessage(models.MessageTypeEvent, "fx", data)
	rt.bus.Publish(topic, msg)
}

func (rt *RoutingTelemetry) record(kind string) {
	rt.emit("routing.record", map[string]any{"kind": kind})
}
'''

# Name-collision trap: the SAME method name `emit` on a receiver that holds a
# bus via an INTERFACE bus.MessageBus satisfies (route 1's interface branch).
IFACE_BUS_EMITTER = '''package fixture

import "github.com/caimlas/meept/pkg/models"

type tinyBus interface {
	Publish(topic string, msg *models.BusMessage) int
}

type Holder struct {
	bus tinyBus
}

func (h *Holder) emit(topic string, data map[string]any) {
	h.bus.Publish(topic, nil)
}

func (h *Holder) record() {
	h.emit("iface.topic", map[string]any{})
}
'''

# A third type in the SAME package that also has an `emit`, but holds no bus.
# Under package-unqualified bare-name matching both would be candidates; the
# receiver-type gate must separate them.
MIXED_PACKAGE = '''package fixture

import "github.com/caimlas/meept/internal/bus"

type Good struct {
	bus *bus.MessageBus
}

func (g *Good) emit(topic string) {
	g.bus.Publish(topic, nil)
}

func (g *Good) record() {
	g.emit("good.topic")
}

type Bad struct {
	ch chan int
}

func (b *Bad) emit(topic string) {
	b.ch <- 1
}

func (b *Bad) record() {
	b.emit("bad.topic")
}
'''

# A seam-only publisher: the receiver holds no bus, but the type carries a
# topic-passing func field whose setter is wired with a closure that really
# publishes to a bus (route 3). Mirrors internal/agent's
# VerificationAutoTrigger.publishEvent EventPublisher, wired in loop.go.
SEAM_ONLY = '''package fixture

import "github.com/caimlas/meept/internal/bus"

type Publisher func(topic string, data map[string]any)

type Trigger struct {
	publishEvent Publisher
}

func (h *Trigger) SetEventPublisher(p Publisher) {
	if h != nil && p != nil {
		h.publishEvent = p
	}
}

func (h *Trigger) Record(kind string) {
	h.publishEvent("seam.record", map[string]any{"kind": kind})
}

func wire(t *Trigger, b *bus.MessageBus) {
	t.SetEventPublisher(func(topic string, data map[string]any) {
		b.Publish(topic, nil)
	})
}
'''


def selftest():
    """Regression pins for the structural publish-helper guard.

    PIN 1  a channel-send `emit` does NOT register and yields NO topics
    PIN 2  a bus-holding `emit` DOES register and its call sites yield topics
    PIN 3  pattern-vacuity: the guard REJECTS the known-bad channel emitter
           even though every other signal (name, params, .Publish-shaped
           call in a sibling) matches. A revert to bare-name matching fails
           here.
    PIN 4  receiver types are separated WITHIN ONE package (same method name,
           one bus holder and one channel sender).
    PIN 5  the bus-via-interface route qualifies a helper.
    PIN 6  the wired-func-field seam route qualifies a helper.
    """
    import tempfile

    failures = []

    def pin(name, ok, detail=""):
        status = "PASS" if ok else "FAIL"
        print(f"  [{status}] {name}" + (f"  — {detail}" if detail else ""))
        if not ok:
            failures.append(name)

    with tempfile.TemporaryDirectory() as td:
        tmp = Path(td)

        # PIN 1: the internal/acp shape registers no helper and no topics.
        path = _fixture(tmp, "channel", CHANNEL_EMITTER)
        pubs, helpers = _publishers_for(path.parent)
        pin("PIN 1a channel-send `emit` does NOT register as a publish helper",
            "emit" not in helpers, f"registered={sorted(helpers)}")
        pin("PIN 1b channel-send `emit` call sites produce NO topics",
            not pubs, f"produced={sorted(pubs)}")

        # PIN 2: the internal/agent shape DOES register and DOES publish.
        path = _fixture(tmp, "bus", BUS_EMITTER)
        pubs, helpers = _publishers_for(path.parent)
        pin("PIN 2a bus-holding `emit` DOES register as a publish helper",
            "emit" in helpers, f"registered={sorted(helpers)}")
        pin("PIN 2b bus-holding `emit` literal-topic call sites DO produce topics",
            "routing.record" in pubs, f"produced={sorted(pubs)}")
        pin("PIN 2c the produced publisher is attributed to the bus fixture",
            all(recv == "rt" for _ln, recv in pubs.get("routing.record", [])),
            f"{pubs.get('routing.record')}")

        # PIN 3: pattern-vacuity. The fixture deliberately satisfies EVERY
        # name/param/sibling-based heuristic a reverted implementation would
        # lean on: the method is named `emit`, the emitted value is a parameter,
        # the file calls `.Publish(` somewhere - yet the receiver holds only a
        # channel. A name-based guard accepts it and fabricates topics; only the
        # structural check rejects it, so a revert fails here.
        path = _fixture(tmp, "vacuity", CHANNEL_EMITTER_WITH_SIBLING)
        idx = _scan_index([path])
        lines = path.read_text().splitlines()
        source = "\n".join(lines)
        emit_hdr = next(
            i for i, l in enumerate(lines) if l.startswith("func (s *Session) emit")
        )
        name_signal = bool(re.search(r'\.emit\(', source))
        param_signal = "ev" in _func_params(lines[emit_hdr])
        sibling_publish = RE_BUS_PUBLISH_CALL.search(source) is not None
        bus_free = not any(BUS_IMPORT in l for l in lines)
        # ...yet the structural gate must still reject it at BOTH gates.
        recv_type = _receiver_type(idx, lines, emit_hdr)
        reg_ok = _helper_can_publish(idx, path, lines, emit_hdr + 1)
        call_line = next(
            i for i, l in enumerate(lines) if ".emit(SessionEvent" in l
        )
        call_ok = _receiver_is_bus_publisher(idx, path, lines, call_line, "s")
        pubs_v, helpers_v = _publishers_for(path.parent)
        pin("PIN 3a the known-bad fixture trips every name-based signal "
            "(fixture is non-vacuous)",
            name_signal and param_signal and sibling_publish and bus_free,
            f"name={name_signal} param={param_signal} "
            f"sibling_publish={sibling_publish} bus_free={bus_free}")
        pin("PIN 3b registration gate REJECTS the channel emitter",
            not reg_ok, f"recv_type={recv_type} accepted={reg_ok}")
        pin("PIN 3c call-site gate REJECTS the channel emitter",
            not call_ok, f"accepted={call_ok}")
        pin("PIN 3d end to end, the bad fixture yields NO helper and NO topics "
            "(a name-based revert would yield both)",
            "emit" not in helpers_v and not pubs_v,
            f"helpers={sorted(helpers_v)} topics={sorted(pubs_v)}")

        # PIN 4: same method name, two receivers, one package.
        path = _fixture(tmp, "mixed", MIXED_PACKAGE)
        pubs, helpers = _publishers_for(path.parent)
        pin("PIN 4a same-named `emit` registers once (the bus holder qualifies)",
            "emit" in helpers, f"registered={sorted(helpers)}")
        pin("PIN 4b the bus holder's call site publishes its topic",
            "good.topic" in pubs, f"produced={sorted(pubs)}")
        pin("PIN 4c the channel sender's call site publishes NOTHING",
            "bad.topic" not in pubs,
            f"bad.topic present={('bad.topic' in pubs)}")

        # PIN 5: the bus-via-interface route.
        path = _fixture(tmp, "iface", IFACE_BUS_EMITTER)
        pubs, helpers = _publishers_for(path.parent)
        pin("PIN 5a a helper holding only a bus-shaped INTERFACE qualifies",
            "emit" in helpers, f"registered={sorted(helpers)}")
        pin("PIN 5b its call site produces its topic",
            "iface.topic" in pubs, f"produced={sorted(pubs)}")

        # PIN 6: the wired-func-field seam route. The receiver holds no bus,
        # but `Trigger` carries a topic-passing func field whose setter is
        # wired with a closure that really publishes. A method on `Trigger`
        # that forwards its topic argument through that field IS a
        # publish-through helper and its call sites are real publishers -
        # this is the shape behind internal/agent's agent.model_escalated.
        path = _fixture(tmp, "seam", SEAM_ONLY)
        pubs, helpers = _publishers_for(path.parent)
        idx6 = _scan_index([path])
        trigger_is_bus_reaching = _bus_holds_bus(idx6, path.parent, "Trigger")
        pin("PIN 6a a bus-less receiver with a wired topic-func seam counts as "
            "bus-reaching", trigger_is_bus_reaching,
            f"seams={sorted(str(s[1]) for s in idx6['seams'])}")
        pin("PIN 6b a seam-forwarding method IS registered as a publish-through "
            "helper", "publishEvent" in helpers,
            f"helpers={sorted(helpers)}")
        pin("PIN 6c its literal-topic call site DOES produce that topic",
            "seam.record" in pubs, f"produced={sorted(pubs)}")

    print()
    if failures:
        print(f"❌ selftest: {len(failures)} pin(s) FAILED: {', '.join(failures)}")
        return 1
    print("✅ selftest: all publish-helper regression pins pass")
    return 0


if __name__ == "__main__":
    if "--selftest" in sys.argv:
        sys.exit(selftest())
    main()
