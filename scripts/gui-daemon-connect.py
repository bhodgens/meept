#!/usr/bin/env python3
"""GUI <-> daemon connect-path tooling for the meept install flow.

The Flutter GUI (ui/flutter_ui) hard-connects to a *fixed* endpoint:
`https://<host>:<port><ws_path>` (TLS, self-signed cert, API-key auth —
see lib/core/constants.dart, lib/services/websocket_service.dart,
lib/services/daemon_cert_pinner.dart). Nothing in the client can discover the
daemon's transport settings, so `make install` must make the two agree:

  * the daemon config ($MEEPT_HOME/meept.json5) must enable transport.http and
    expose REST + WebSocket;
  * the GUI build must embed the endpoint the daemon actually binds
    (--dart-define MEEPT_API_HOST/MEEPT_API_PORT/MEEPT_WS_PATH) and the key the
    daemon actually accepts (--dart-define MEEPT_DEV_API_KEY).

Subcommands
-----------
  ensure-config   Idempotently make $MEEPT_HOME/meept.json5 GUI-ready: insert
                  transport.http.{enabled,rest,websocket} = true and
                  ws_path = "/ws" when absent/false. Never touches addr, TLS,
                  require_auth, api_keys or anything unrelated; backs the file
                  up; validates the result and rolls back if it is not
                  structurally sound JSON5.
  endpoint        Print the host/port/ws_path the GUI must use, derived from
                  the daemon config with the daemon's own precedence —
                  transport.http.addr, else the transport.http.port alias as
                  127.0.0.1:<port>, else the shipped default localhost:8081
                  (--json also reports port_source).
  check           Static PASS/FAIL/SKIP checks for the installed connect path.
  probe           Live client probe against a running daemon: TLS +
                  cert-pin check, /api/v1/health, WebSocket upgrade with the
                  GUI key (expect 101) and with a bogus key (expect 401/418).

Exit codes: 0 all checks pass, 1 a check failed, 3 skipped/unavailable.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import re
import shutil
import socket
import ssl
import sys
import time
from pathlib import Path

# ---------------------------------------------------------------------------
# Paths / config resolution
# ---------------------------------------------------------------------------

BANNED_PUBLIC_KEYS = {
    # internal/comm/http/server.go bannedPublicAPIKeys — the daemon refuses to
    # start its HTTP server when one of these is configured, and the old
    # Makefile fallback injected the first one into the GUI build.
    "meept_dev_default_key_CHANGE_ME",
    "d@ng3r_NOT_A_Secure_key_REGENERATE_M3",
}

DEFAULT_HOST = "localhost"
DEFAULT_PORT = 8081
DEFAULT_WS_PATH = "/ws"
LOOPBACK_HOSTS = {"localhost", "127.0.0.1", "::1"}


def meept_home() -> Path:
    override = os.environ.get("MEEPT_HOME", "").strip()
    if override:
        return Path(os.path.expanduser(override))
    return Path.home() / ".meept"


def config_path() -> Path:
    return meept_home() / "meept.json5"


# ---------------------------------------------------------------------------
# Minimal JSON5 structure scanner (comment/string aware)
# ---------------------------------------------------------------------------


def strip_comments(text: str) -> str:
    """Blank out // and /* */ comments, preserving length and strings."""
    out = list(text)
    i, n = 0, len(text)
    in_str = False
    quote = ""
    while i < n:
        c = text[i]
        if in_str:
            if c == "\\":
                i += 2
                continue
            if c == quote:
                in_str = False
            i += 1
            continue
        if c in "\"'":
            in_str = True
            quote = c
            i += 1
            continue
        if c == "/" and i + 1 < n and text[i + 1] == "/":
            while i < n and text[i] != "\n":
                out[i] = " "
                i += 1
            continue
        if c == "/" and i + 1 < n and text[i + 1] == "*":
            out[i] = out[i + 1] = " "
            i += 2
            while i < n - 1 and not (text[i] == "*" and text[i + 1] == "/"):
                if text[i] != "\n":
                    out[i] = " "
                i += 1
            if i < n - 1:
                out[i] = out[i + 1] = " "
                i += 2
            continue
        i += 1
    return "".join(out)


def quote_mask(text: str) -> str:
    """Blank out comment *and string* contents; braces only survive as structure."""
    stripped = strip_comments(text)  # comments -> spaces, so apostrophes in
    # comments can no longer be mistaken for string delimiters below.
    out = list(stripped)
    i, n = 0, len(stripped)
    in_str = False
    quote = ""
    while i < n:
        c = stripped[i]
        if in_str:
            if c == "\\":
                if i + 1 < n:
                    out[i] = out[i + 1] = " "
                i += 2
                continue
            if c == quote:
                in_str = False
            else:
                out[i] = " "
            i += 1
            continue
        if c in "\"'":
            in_str = True
            quote = c
        i += 1
    return "".join(out)


def match_brace(mask: str, open_idx: int) -> int:
    """Index of the '}' matching the '{' at open_idx (both must be in mask)."""
    if mask[open_idx] != "{":
        raise ValueError(f"expected '{{' at {open_idx}")
    depth = 0
    for i in range(open_idx, len(mask)):
        if mask[i] == "{":
            depth += 1
        elif mask[i] == "}":
            depth -= 1
            if depth == 0:
                return i
    raise ValueError("unbalanced braces")


def object_span_for_key(text: str, mask: str, key: str, start: int = 0, end: int | None = None):
    """(obj_start, obj_end) for `"key": { ... }` inside [start, end)."""
    end = len(text) if end is None else end
    clean = strip_comments(text)
    for m in re.finditer(r'"' + re.escape(key) + r'"\s*:\s*\{', clean[start:end]):
        abs_start = start + m.start()
        brace = clean.index("{", start + m.start())
        try:
            close = match_brace(mask, brace)
        except ValueError:
            continue
        if close < end:
            return brace, close
    return None


def scalar_keys(text: str, mask: str, obj_start: int, obj_end: int) -> dict:
    """Top-level scalar keys of the object spanning (obj_start, obj_end)."""
    clean = strip_comments(text)
    found: dict[str, str] = {}
    i = obj_start + 1
    depth = 0
    while i < obj_end:
        c = mask[i]
        if c in "{[":
            depth += 1
        elif c in "}]":
            depth -= 1
        elif depth == 0:
            m = re.match(r'"([A-Za-z0-9_]+)"\s*:\s*([^,\n}]+)', clean[i:])
            if m:
                found[m.group(1)] = m.group(2).strip()
                i += m.end()
                continue
        i += 1
    return found


def _find_key_offset(clean: str, mask: str, key: str, start: int, end: int) -> tuple[int, int] | None:
    """(key_start, value_start) of a top-level `"key":` inside [start,end)."""
    depth = 0
    i = start
    while i < end:
        c = mask[i]
        if c in "{[":
            depth += 1
        elif c in "}]":
            depth -= 1
        elif depth == 0 and mask[i] == '"':
            m = re.compile(r'"([A-Za-z0-9_]+)"\s*:').match(clean, i)
            if m:
                if m.group(1) == key:
                    return m.start(), m.end()
                i = m.end()
                continue
        i += 1
    return None


INNER_INDENT = "      "  # matches the shipped template's transport.http block


def _indent_of_line(text: str, idx: int, fallback: str = INNER_INDENT) -> str:
    line_start = text.rfind("\n", 0, idx) + 1
    line = text[line_start:idx]
    m = re.match(r"[ \t]*", line)
    return m.group(0) if m else fallback


def ensure_http_block(text: str) -> tuple[str, list[str]]:
    """Return (new_text, changes). Idempotent; text-only edits, comments kept."""
    changes: list[str] = []
    mask = quote_mask(text)

    transport = object_span_for_key(text, mask, "transport")
    if transport is None:
        root_open = mask.find("{")
        root_close = mask.rfind("}")
        if root_open < 0 or root_close <= root_open:
            raise ValueError("no JSON object at the root of the config")
        block = (
            "\n  // Added by `make install` (gui-connect-setup): the Flutter GUI and\n"
            "  // other HTTP clients need the HTTP transport. TLS and API-key auth stay on.\n"
            "  \"transport\": {\n"
            "    \"http\": {\n"
            f"      \"enabled\": true,\n"
            f"      \"addr\": \"127.0.0.1:{DEFAULT_PORT}\",\n"
            "      \"require_auth\": true,\n"
            "      \"rest\": true,\n"
            "      \"websocket\": true,\n"
            f"      \"ws_path\": \"{DEFAULT_WS_PATH}\",\n"
            "    },\n"
            "  },\n"
        )
        text = text[:root_close] + block + text[root_close:]
        changes.append("inserted transport.http block (enabled, addr 127.0.0.1:%d)" % DEFAULT_PORT)
        return text, changes

    t_start, t_end = transport
    http = object_span_for_key(text, mask, "http", t_start, t_end)
    if http is None:
        indent = _indent_of_line(text, t_start)
        block = (
            f"\n{indent}  \"http\": {{\n"
            f"{indent}    \"enabled\": true,\n"
            f"{indent}    \"addr\": \"127.0.0.1:{DEFAULT_PORT}\",\n"
            f"{indent}    \"require_auth\": true,\n"
            f"{indent}    \"rest\": true,\n"
            f"{indent}    \"websocket\": true,\n"
            f"{indent}    \"ws_path\": \"{DEFAULT_WS_PATH}\",\n"
            f"{indent}  }},\n"
        )
        text = text[: t_start + 1] + block + text[t_start + 1 :]
        changes.append("inserted transport.http block")
        return text, changes

    h_start, h_end = http

    def set_scalar(key: str, value: str, why: str) -> None:
        nonlocal text, mask, h_start, h_end
        # h_start is the '{' of the http object: scan from the first inner char.
        loc = _find_key_offset(strip_comments(text), mask, key, h_start + 1, h_end)
        if loc is None:
            indent = (_indent_of_line(text, h_start) or INNER_INDENT) + "  "
            insert_at = h_start + 1
            text = text[:insert_at] + f'\n{indent}"{key}": {value},' + text[insert_at:]
            mask = quote_mask(text)
            span = object_span_for_key(text, mask, "http")
            h_start, h_end = span if span else (h_start, h_end)
            changes.append(f"added {why}")
            return
        _, value_start = loc
        m = re.match(r'("(?:[^"\\]|\\.)*"|[A-Za-z0-9_.+-]+)', text[value_start:])
        if m:
            current = m.group(1)
            if current != value:
                text = text[:value_start] + value + text[value_start + len(current):]
                mask = quote_mask(text)
                span = object_span_for_key(text, mask, "http")
                h_start, h_end = span if span else (h_start, h_end)
                changes.append(f"set {why} (was {current})")
        else:
            changes.append(f"left {why} as-is (unrecognised value)")

    set_scalar("enabled", "true", "transport.http.enabled=true")
    set_scalar("rest", "true", "transport.http.rest=true")
    set_scalar("websocket", "true", "transport.http.websocket=true")
    set_scalar("ws_path", f'"{DEFAULT_WS_PATH}"', f'transport.http.ws_path="{DEFAULT_WS_PATH}"')
    return text, changes


def validate_json5_like(text: str) -> None:
    """Structural validation: balanced braces/brackets, strings, and our keys."""
    mask = quote_mask(text)
    depth_curly = depth_square = 0
    for c in mask:
        if c == "{":
            depth_curly += 1
        elif c == "}":
            depth_curly -= 1
        elif c == "[":
            depth_square += 1
        elif c == "]":
            depth_square -= 1
        if depth_curly < 0 or depth_square < 0:
            raise ValueError("unbalanced brackets")
    if depth_curly or depth_square:
        raise ValueError("unbalanced brackets")
    # Must still contain a transport.http object with enabled: true.
    span = object_span_for_key(text, mask, "transport")
    if span is None:
        raise ValueError("transport block missing after edit")
    http = object_span_for_key(text, mask, "http", span[0], span[1])
    if http is None:
        raise ValueError("transport.http missing after edit")
    keys = scalar_keys(text, mask, http[0], http[1])
    if keys.get("enabled") != "true":
        raise ValueError("transport.http.enabled is not true after edit")


def cmd_ensure_config(args: argparse.Namespace) -> int:
    cfg = config_path()
    result: dict = {"config": str(cfg), "changed": False, "changes": [], "skipped": None}

    toml = meept_home() / "meept.toml"
    if not cfg.exists() and toml.exists():
        result["skipped"] = (
            f"{toml} exists and no meept.json5 — refusing to rewrite TOML. "
            "Enable transport.http (enabled/rest/websocket/ws_path) manually."
        )
        _emit(result, args)
        return 3

    if not cfg.exists():
        repo_template = Path(__file__).resolve().parent.parent / "config" / "meept.json5"
        cfg.parent.mkdir(parents=True, exist_ok=True)
        if repo_template.exists():
            shutil.copyfile(repo_template, cfg)
            result["changes"].append(f"created {cfg} from {repo_template}")
        else:
            cfg.write_text("{\n}\n")
            result["changes"].append(f"created empty {cfg}")
        os.chmod(cfg, 0o600)

    original = cfg.read_text()
    try:
        updated, changes = ensure_http_block(original)
        validate_json5_like(updated)
    except Exception as exc:  # noqa: BLE001 - report, never write a broken config
        result["skipped"] = f"config left untouched: {exc}"
        _emit(result, args)
        return 3

    if updated != original:
        backup = cfg.with_suffix(cfg.suffix + f".bak-gui-connect-{int(time.time())}")
        shutil.copyfile(cfg, backup)
        tmp = cfg.with_suffix(cfg.suffix + ".tmp")
        tmp.write_text(updated)
        os.chmod(tmp, 0o600)
        os.replace(tmp, cfg)
        result["changed"] = True
        result["changes"] = changes
        result["backup"] = str(backup)
    _emit(result, args)
    return 0


# ---------------------------------------------------------------------------
# Endpoint derivation
# ---------------------------------------------------------------------------


def parse_addr(addr: str) -> tuple[str, int]:
    raw = (addr or "").strip()
    if not raw:
        return DEFAULT_HOST, DEFAULT_PORT
    host, _, port_s = raw.rpartition(":")
    host = host.strip().strip("[]")
    try:
        port = int(port_s)
    except ValueError:
        return DEFAULT_HOST, DEFAULT_PORT
    if host in ("", "0.0.0.0", "::", "*"):
        host = DEFAULT_HOST
    return host or DEFAULT_HOST, port


def read_http_block() -> dict:
    cfg = config_path()
    if not cfg.exists():
        return {}
    text = cfg.read_text()
    mask = quote_mask(text)
    transport = object_span_for_key(text, mask, "transport")
    if transport is None:
        return {}
    http = object_span_for_key(text, mask, "http", transport[0], transport[1])
    if http is None:
        return {}
    keys = scalar_keys(text, mask, http[0], http[1])
    parsed: dict = {}
    for k, v in keys.items():
        v = v.strip()
        if v == "true":
            parsed[k] = True
        elif v == "false":
            parsed[k] = False
        else:
            m = re.match(r'^"((?:[^"\\]|\\.)*)"$', v)
            parsed[k] = m.group(1) if m else v
    # Which keys the user actually wrote (absent keys fall back to
    # internal/config DefaultConfig, not to "false").
    parsed["_present"] = sorted(keys.keys())
    return parsed


def cert_path_from_http(http: dict) -> str:
    """Resolve the daemon's TLS cert path the way the daemon does.

    config transport.http.tls_cert_file when set; otherwise internal/config
    DefaultConfig's ~/.meept/certs/tls.crt, then the shipped template's
    ~/.meept/tls/cert.pem as a fallback candidate.
    """
    explicit = http.get("tls_cert_file")
    if explicit:
        return os.path.expanduser(str(explicit))
    candidates = [meept_home() / "certs" / "tls.crt", meept_home() / "tls" / "cert.pem"]
    for cand in candidates:
        if cand.exists():
            return str(cand)
    return str(candidates[0])


def effective_endpoint() -> dict:
    http = read_http_block()
    present = set(http.get("_present", []))
    # Precedence mirrors the daemon's internal/config
    # HTTPTransportConfig.ListenAddr(): Addr when set, else "127.0.0.1:<Port>"
    # when the `port` alias is set, else the shipped default. Resolving Addr
    # only dropped the alias, so a port-only config (docs/configuration/
    # production-security.md) had this tooling checking :8081 while the daemon
    # bound the alias port (audit F30).
    addr = str(http.get("addr", "") or "").strip()
    port_source = "default"
    port_alias_inert = False
    if addr:
        host, port = parse_addr(addr)
        port_source = "addr"
        alias = str(http.get("port", "") or "").strip()
        try:
            alias_port = int(alias) if alias else 0
        except ValueError:
            alias_port = 0
        # The daemon ignores `port` whenever `addr` is set (ListenAddr), so a
        # disagreeing alias is inert config the operator should know about.
        if alias_port > 0 and alias_port != port:
            port_alias_inert = True
    else:
        host, port = DEFAULT_HOST, DEFAULT_PORT
        alias = str(http.get("port", "") or "").strip()
        try:
            alias_port = int(alias) if alias else 0
        except ValueError:
            alias_port = 0
        if alias_port > 0:
            port = alias_port
            port_source = "port-alias"
    ws_path = str(http.get("ws_path") or DEFAULT_WS_PATH)
    if not ws_path.startswith("/"):
        ws_path = "/" + ws_path
    return {
        "host": host,
        "port": port,
        "port_source": port_source,
        "port_alias_inert": port_alias_inert,
        "ws_path": ws_path,
        "cert_file": cert_path_from_http(http),
        # Absent -> internal/config DefaultConfig values (enabled=false,
        # rest=false, websocket=false, require_auth=TRUE — schema.go).
        "enabled": http.get("enabled") is True,
        "rest": http.get("rest") is True,
        "websocket": http.get("websocket") is True,
        "require_auth": http["require_auth"] if "require_auth" in present else True,
        "require_auth_explicit": "require_auth" in present,
        "present_keys": sorted(present),
    }


def cmd_endpoint(args: argparse.Namespace) -> int:
    ep = effective_endpoint()
    if args.field:
        print(ep.get(args.field, ""))
    else:
        print(json.dumps(ep, indent=2) if args.json else f"{ep['host']}:{ep['port']}{ep['ws_path']}")
    return 0


# ---------------------------------------------------------------------------
# Live probes (mirrors the Flutter client's connect path)
# ---------------------------------------------------------------------------


def cert_fingerprint_from_pem(path: str) -> str | None:
    try:
        pem = Path(path).read_text()
    except OSError:
        return None
    body = "".join(l for l in pem.splitlines() if not l.startswith("-----")).strip()
    try:
        der = base64.b64decode(body)
    except Exception:  # noqa: BLE001
        return None
    return hashlib.sha256(der).hexdigest()


def tls_connect(endpoint: dict, timeout: float) -> tuple[ssl.SSLSocket, dict]:
    """TLS handshake; returns (socket, info) mirroring DaemonCertPinner rules."""
    info: dict = {}
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    raw = socket.create_connection((endpoint["host"], endpoint["port"]), timeout=timeout)
    tls = ctx.wrap_socket(raw, server_hostname=endpoint["host"])
    der = tls.getpeercert(binary_form=True)
    if not der:
        raise ssl.SSLError("TLS handshake completed without a peer certificate")
    presented = hashlib.sha256(der).hexdigest()
    expected = cert_fingerprint_from_pem(endpoint["cert_file"])
    info["presented_fingerprint"] = presented
    info["expected_fingerprint"] = expected
    if expected is not None:
        info["pinned"] = presented == expected
    else:
        # DaemonCertPinner accepts any certificate for localhost when the PEM is
        # unreadable (sandbox / missing file).
        info["pinned"] = endpoint["host"] in LOOPBACK_HOSTS
        info["note"] = f"no readable cert at {endpoint['cert_file']}; localhost-only trust"
    return tls, info


def http_get(tls: ssl.SSLSocket, endpoint: dict, path: str, key: str | None,
             extra_headers: list[str] | None = None,
             connection_close: bool = True) -> tuple[int, str]:
    lines = [
        f"GET {path} HTTP/1.1",
        f"Host: {endpoint['host']}:{endpoint['port']}",
    ]
    if connection_close:
        # Must be omitted for a WebSocket upgrade: a second Connection header
        # makes the server read "close" and answer 400.
        lines.append("Connection: close")
    if key:
        lines.append(f"Authorization: Bearer {key}")
    lines += extra_headers or []
    tls.sendall(("\r\n".join(lines) + "\r\n\r\n").encode())
    data = b""
    tls.settimeout(10)
    try:
        while b"\r\n\r\n" not in data and len(data) < 65536:
            chunk = tls.recv(4096)
            if not chunk:
                break
            data += chunk
    except (socket.timeout, TimeoutError):
        pass
    head = data.split(b"\r\n\r\n", 1)[0].decode("latin-1", "replace")
    first = head.splitlines()[0] if head else ""
    m = re.match(r"HTTP/1\.[01] (\d{3})", first)
    return (int(m.group(1)) if m else 0), head


def ws_handshake(tls: ssl.SSLSocket, endpoint: dict, key: str | None) -> tuple[int, str]:
    nonce = base64.b64encode(os.urandom(16)).decode()
    extra = [
        "Upgrade: websocket",
        "Connection: Upgrade",
        "Sec-WebSocket-Version: 13",
        f"Sec-WebSocket-Key: {nonce}",
        f"Origin: https://{endpoint['host']}:{endpoint['port']}",
    ]
    return http_get(tls, endpoint, endpoint["ws_path"], key,
                    extra_headers=extra, connection_close=False)


def cmd_probe(args: argparse.Namespace) -> int:
    ep = effective_endpoint()
    host = args.host or ep["host"]
    port = args.port or ep["port"]
    ep.update({"host": host, "port": port, "ws_path": args.ws_path or ep["ws_path"]})
    if args.cert_file:
        ep["cert_file"] = args.cert_file

    key = args.key
    if key is None and args.key_file:
        try:
            key = Path(args.key_file).read_text().strip()
        except OSError as exc:
            print(f"SKIP  api-key — cannot read {args.key_file}: {exc}")
            return 3

    results: list[tuple[str, str, str]] = []

    def add(status: str, name: str, detail: str) -> None:
        results.append((status, name, detail))

    # 1. TCP + TLS + cert pin
    try:
        tls, info = tls_connect(ep, args.timeout)
    except Exception as exc:  # noqa: BLE001
        add("FAIL", "tcp+tls", f"cannot reach https://{ep['host']}:{ep['port']} — {exc}")
        _print_results(results)
        return 1
    if info.get("pinned"):
        detail = f"fingerprint {info['presented_fingerprint'][:16]}…"
        if info.get("note"):
            detail += f" ({info['note']})"
        add("PASS", "tcp+tls+cert", detail)
    else:
        add("FAIL", "tcp+tls+cert",
            f"presented {info['presented_fingerprint'][:16]}… but "
            f"{ep['cert_file']} pins {str(info['expected_fingerprint'])[:16]}…")
    tls.close()

    # 2. REST health (unauthenticated by design)
    try:
        tls, _ = tls_connect(ep, args.timeout)
        code, head = http_get(tls, ep, "/api/v1/health", key)
        tls.close()
        if code == 200:
            add("PASS", "http /api/v1/health", "200")
        else:
            add("FAIL", "http /api/v1/health", f"status {code}")
    except Exception as exc:  # noqa: BLE001
        add("FAIL", "http /api/v1/health", str(exc))

    # 3. WebSocket upgrade with the GUI key -> 101
    try:
        tls, _ = tls_connect(ep, args.timeout)
        code, head = ws_handshake(tls, ep, key)
        tls.close()
        if code == 101:
            add("PASS", f"ws {ep['ws_path']} (gui key)", "101 Switching Protocols")
        elif code in (401, 418):
            add("FAIL", f"ws {ep['ws_path']} (gui key)",
                f"{code} — daemon rejected the GUI's API key")
        else:
            add("FAIL", f"ws {ep['ws_path']} (gui key)", f"status {code}")
    except Exception as exc:  # noqa: BLE001
        add("FAIL", f"ws {ep['ws_path']} (gui key)", str(exc))

    # 4. WebSocket upgrade with a bogus key -> auth must be enforced
    try:
        tls, _ = tls_connect(ep, args.timeout)
        code, head = ws_handshake(tls, ep, "definitely-not-a-valid-key")
        tls.close()
        if code in (401, 418):
            add("PASS", "ws auth enforced (bogus key)", f"rejected with {code}")
        elif code == 101:
            add("FAIL", "ws auth enforced (bogus key)",
                "daemon accepted an invalid key (auth bypass!)")
        else:
            add("FAIL", "ws auth enforced (bogus key)", f"unexpected status {code}")
    except Exception as exc:  # noqa: BLE001
        add("FAIL", "ws auth enforced (bogus key)", str(exc))

    _print_results(results)
    return 0 if all(r[0] == "PASS" for r in results) else 1


# ---------------------------------------------------------------------------
# Static checks
# ---------------------------------------------------------------------------


def cmd_check(args: argparse.Namespace) -> int:
    ep = effective_endpoint()
    results: list[tuple[str, str, str]] = []

    def add(status: str, name: str, detail: str) -> None:
        results.append((status, name, detail))

    cfg = config_path()
    if not cfg.exists():
        add("FAIL", "config file", f"{cfg} missing — run `make install` / `make gui-connect-setup`")
    else:
        add("PASS", "config file", str(cfg))
        add("PASS" if ep["enabled"] else "FAIL", "transport.http.enabled",
            "true" if ep["enabled"] else "false — GUI/WS clients cannot connect")
        add("PASS" if ep["rest"] else "FAIL", "transport.http.rest",
            "true" if ep["rest"] else "false")
        add("PASS" if ep["websocket"] else "FAIL", "transport.http.websocket",
            "true" if ep["websocket"] else "false")
        add("PASS" if ep["ws_path"] else "FAIL", "transport.http.ws_path", ep["ws_path"])
        if ep["require_auth"]:
            detail = "true (API-key auth enforced)" if ep["require_auth_explicit"] \
                else "true (unset -> DefaultConfig default)"
            add("PASS", "transport.http.require_auth", detail)
        else:
            add("FAIL", "transport.http.require_auth",
                "false — the local API is unauthenticated")
        if ep["host"] in LOOPBACK_HOSTS:
            add("PASS", "GUI host is loopback", ep["host"])
        else:
            add("SKIP", "GUI host is loopback",
                f"{ep['host']} — the bundled cert pinner only trusts localhost/127.0.0.1/::1")

        # Which config key produced the port (mirrors the daemon's ListenAddr
        # precedence: addr > port alias > shipped default). A port-only config
        # used to be resolved as :8081 here while the daemon bound the alias —
        # this line makes that class of divergence visible.
        src = ep.get("port_source", "default")
        if src == "addr":
            detail = f"transport.http.addr = {ep['host']}:{ep['port']}"
            if ep.get("port_alias_inert"):
                add("SKIP", "endpoint source",
                    detail + " (transport.http.port is set but inert while addr is set)")
            else:
                add("PASS", "endpoint source", detail)
        elif src == "port-alias":
            add("PASS", "endpoint source",
                f"transport.http.port alias = 127.0.0.1:{ep['port']} (addr unset)")
        else:
            add("PASS", "endpoint source",
                f"neither addr nor port set — shipped default {ep['host']}:{ep['port']}")

    key_file = meept_home() / "dev_key"
    if key_file.exists() and key_file.read_text().strip():
        key = key_file.read_text().strip()
        mode = oct(key_file.stat().st_mode & 0o777)
        add("PASS", "dev key file", f"{key_file} (mode {mode})")
        if mode != "0o600":
            add("FAIL", "dev key permissions", f"{mode} — expected 0o600")
        if key in BANNED_PUBLIC_KEYS:
            add("FAIL", "dev key is not a public default",
                "the file holds a publicly-known legacy key")
        else:
            add("PASS", "dev key is not a public default", f"{len(key)} chars")
    else:
        add("FAIL", "dev key file",
            f"{key_file} missing — `make dev-key` provisions it (the daemon also creates it at startup)")

    _print_results(results)
    return 0 if all(r[0] != "FAIL" for r in results) else 1


# ---------------------------------------------------------------------------
# Plumbing
# ---------------------------------------------------------------------------


def _print_results(results: list[tuple[str, str, str]]) -> None:
    for status, name, detail in results:
        print(f"{status}  {name} — {detail}")


def _emit(result: dict, args: argparse.Namespace) -> None:
    if getattr(args, "json", False):
        print(json.dumps(result, indent=2))
        return
    if result.get("changes") is None:
        result["changes"] = []
    for change in result.get("changes", []):
        print(f"  {change}")
    if result.get("backup"):
        print(f"  backup: {result['backup']}")
    if result.get("skipped"):
        print(f"  SKIP: {result['skipped']}")
    if not result.get("changed") and not result.get("skipped"):
        print(f"  {result['config']} already GUI-ready (no changes)")


def main(argv: list[str] | None = None) -> int:
    doc = (__doc__ or "GUI <-> daemon connect-path tooling").splitlines()[0]
    parser = argparse.ArgumentParser(description=doc)
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("ensure-config", help="make $MEEPT_HOME/meept.json5 GUI-ready")
    p.add_argument("--json", action="store_true")
    p.set_defaults(func=cmd_ensure_config)

    p = sub.add_parser("endpoint", help="print the endpoint the GUI must use")
    p.add_argument("--field", choices=["host", "port", "ws_path", "cert_file"])
    p.add_argument("--json", action="store_true")
    p.set_defaults(func=cmd_endpoint)

    p = sub.add_parser("check", help="static checks of the installed connect path")
    p.add_argument("--json", action="store_true")
    p.set_defaults(func=cmd_check)

    p = sub.add_parser("probe", help="live TLS/HTTP/WS probe against a running daemon")
    p.add_argument("--host")
    p.add_argument("--port", type=int)
    p.add_argument("--ws-path")
    p.add_argument("--cert-file")
    p.add_argument("--key", help="API key to present (defaults to --key-file)")
    p.add_argument("--key-file", help="file holding the API key (default: $MEEPT_HOME/dev_key)")
    p.add_argument("--timeout", type=float, default=5.0)
    p.set_defaults(func=cmd_probe)

    args = parser.parse_args(argv)

    if args.command == "probe" and args.key is None and args.key_file is None:
        default_key = meept_home() / "dev_key"
        if default_key.exists():
            args.key_file = str(default_key)

    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
