#!/usr/bin/env python3
"""Config drift checker: repo templates vs an installed meept home.

Compares the JSON5 config templates shipped in the repo (config/*.json5)
against the installed copies in $MEEPT_HOME (default ~/.meept) and reports
per-file, per-key drift, classified the way the repo's installed-config
review doctrine classifies it:

  template-defect   the TEMPLATE itself is wrong (stale default, typo,
                    references a path that does not resolve). Fix in config/.
  local-drift       the INSTALL diverged from the template intentionally
                    (operator tuned a value for this machine). Never clobber
                    on sync; the template may legitimately move on.
  correct-by-schema the install ADDS fields the template lacks and the
                    schema allows (newer keys an older template does not
                    know yet). Safe: sync must preserve them.

Read-only by default: this script NEVER writes to either side. It never
fails on unknown keys (JSON5 is a superset of JSON; parsing is tolerant -
// and /* */ comments and trailing commas are stripped before the JSON
parse) and every structural difference it cannot classify is reported.

Usage:
  python3 scripts/config-drift-check.py                      # report
  python3 scripts/config-drift-check.py --repo ~/git/meept --home ~/.meept
  python3 scripts/config-drift-check.py --json               # machine-readable
  python3 scripts/config-drift-check.py --defect foo,bar.baz --local-drift qux
  python3 scripts/config-drift-check.py --strict             # exit 1 on ANY drift

Exit codes:
  0  no drift, or every difference carries a classification comment
  1  unclassified drift found (report mode) / any drift found (--strict)
  2  usage or IO error

Classification is annotation-based so the classification itself is
version-controlled next to the template. A defect that reviewers have
accepted lives in the template as a comment on the affected line:

  // drift: template-defect: reason text here

and accepted local divergence is declared in the TEMPLATE for the file
(one line, anywhere):

  // drift: local-drift: key.subkey -> reason
  // drift: correct-by-schema: -> the home may add schema-valid fields

Anything with no matching annotation is UNCLASSIFIED and fails the run.
"""

import argparse
import json
import os
import re
import sys

TEMPLATE_KEYS = ("meept.json5", "models.json5")


def strip_json5(text):
    """Tolerant JSON5 -> JSON: remove // and /* */ comments, trailing commas.

    String-aware so comment markers inside string literals survive.
    """
    out = []
    i, n = 0, len(text)
    in_str = False
    while i < n:
        c = text[i]
        if in_str:
            out.append(c)
            if c == "\\" and i + 1 < n:
                out.append(text[i + 1])
                i += 2
                continue
            if c == '"':
                in_str = False
            i += 1
            continue
        if c == '"':
            in_str = True
            out.append(c)
            i += 1
            continue
        if c == "/" and i + 1 < n and text[i + 1] == "/":
            while i < n and text[i] != "\n":
                i += 1
            continue
        if c == "/" and i + 1 < n and text[i + 1] == "*":
            j = text.find("*/", i + 2)
            i = n if j < 0 else j + 2
            continue
        out.append(c)
        i += 1
    s = "".join(out)
    # strip trailing commas before } or ]
    s = re.sub(r",(\s*[}\]])", r"\1", s)
    return s


def parse_json5(path):
    with open(path, "r", encoding="utf-8") as f:
        return json.loads(strip_json5(f.read()))


def flat(d, prefix=""):
    """Flatten nested dicts/lists to dotted leaf keys -> value."""
    items = {}
    if isinstance(d, dict):
        for k, v in d.items():
            key = f"{prefix}.{k}" if prefix else k
            if isinstance(v, (dict, list)):
                items.update(flat(v, key))
            else:
                items[key] = v
    elif isinstance(d, list):
        # lists are compared as whole-value leaves at their key
        items[prefix] = d
    return items


def dotted_leaves(obj, path=()):
    """Yield (dotted_path, leaf_value) for every scalar and list leaf."""
    if isinstance(obj, dict):
        for k, v in obj.items():
            yield from dotted_leaves(v, path + (k,))
    elif isinstance(obj, list):
        yield (".".join(path) if path else "<root>", obj)
    else:
        yield (".".join(path) if path else "<root>", obj)


def load_annotations(template_path):
    """Parse // drift: annotations from a template.

    Returns (defects, local_drift, cbs_keys, cbs_all):
      defects: dict dotted-key -> reason (template-defect)
      local_drift: dict dotted-key -> reason
      cbs_keys: set of dotted keys explicitly correct-by-schema
      cbs_all: bool (file-level blanket declaration)
    Key patterns accept a trailing '*' wildcard segment (foo.* matches
    foo.bar and deeper paths under foo).
    """
    defects, local_drift, cbs_keys = {}, {}, set()
    cbs_all = False
    key_re = re.compile(r"^[A-Za-z0-9_.*\[\]-]+")
    with open(template_path, "r", encoding="utf-8") as f:
        for line in f:
            m = re.search(r"//\s*drift:\s*([a-z-]+)\s*:\s*(.*)", line)
            if not m:
                continue
            kind, rest = m.group(1), m.group(2).strip()
            if kind == "template-defect":
                km = key_re.match(rest)
                if km:
                    defects[km.group(0)] = rest[km.end():].strip(" :-")
            elif kind == "local-drift":
                km = key_re.match(rest)
                if km:
                    local_drift[km.group(0)] = rest[km.end():].strip(" :-")
            elif kind == "correct-by-schema":
                if rest.startswith("->") or rest == "":
                    cbs_all = True
                else:
                    km = key_re.match(rest)
                    if km:
                        cbs_keys.add(km.group(0))
    return defects, local_drift, cbs_keys, cbs_all


def matches(pattern, key):
    """Annotation pattern vs dotted key: exact or '*' wildcard segment."""
    if pattern == key:
        return True
    if "*" in pattern:
        parts = pattern.split(".")
        kp = key.split(".")
        for i, p in enumerate(parts):
            if p == "*":
                # matches one-or-more remaining segments
                return len(kp) > i
            if i >= len(kp) or kp[i] != p:
                return False
        return True
    return False


def value_matches(patterns, key):
    return any(matches(p, key) for p in patterns)


def fmt(v, limit=72):
    """Compact one-line repr for the report table."""
    s = repr(v)
    if len(s) > limit:
        s = s[:limit - 3] + "..."
    return s


class DriftRecord:
    def __init__(self, file_name, key, kind, detail):
        self.file_name = file_name
        self.key = key
        self.kind = kind
        self.detail = detail


def compare_file(name, repo, home, strict_extra=()):
    """Diff one template against its installed copy.

    Returns (records, error_or_None).
    """
    t_path = os.path.join(repo, "config", name)
    h_path = os.path.join(home, name)
    if not os.path.exists(h_path):
        return [], f"installed copy missing: {h_path}"
    if not os.path.exists(t_path):
        return [], f"template missing: {t_path}"
    try:
        t_obj = parse_json5(t_path)
        h_obj = parse_json5(h_path)
    except (ValueError, OSError) as e:
        return [], f"parse failure ({name}): {e}"

    defects, local_drift, cbs_keys, cbs_all = load_annotations(t_path)

    t_leaves = dict(dotted_leaves(t_obj))
    h_leaves = dict(dotted_leaves(h_obj))

    records = []
    defect_pats, ld_pats, cbs_pats = list(defects), list(local_drift), list(cbs_keys)

    # 1. keys present in template, different/absent in install
    for key, t_val in sorted(t_leaves.items()):
        if key not in h_leaves:
            if value_matches(ld_pats, key):
                continue  # annotated: install may drop it
            records.append(DriftRecord(name, key, "UNCLASSIFIED",
                             f"missing in install (template value: {fmt(t_val)})"))
        elif h_leaves[key] != t_val:
            if value_matches(defect_pats, key):
                records.append(DriftRecord(name, key, "template-defect",
                                 f"template={fmt(t_val)} install={fmt(h_leaves[key])} :: {defects.get(key, defects.get('*', ''))}"))
            elif value_matches(ld_pats, key):
                records.append(DriftRecord(name, key, "local-drift",
                                 f"template={fmt(t_val)} install={fmt(h_leaves[key])} :: {local_drift.get(key, '')}"))
            else:
                records.append(DriftRecord(name, key, "UNCLASSIFIED",
                                 f"template={fmt(t_val)} install={fmt(h_leaves[key])}"))

    # 2. keys only in the install (schema-allowed additions vs unexplained)
    for key, h_val in sorted(h_leaves.items()):
        if key in t_leaves:
            continue
        if cbs_all or value_matches(cbs_pats, key):
            records.append(DriftRecord(name, key, "correct-by-schema",
                             f"install-only: {fmt(h_val)}"))
        else:
            records.append(DriftRecord(name, key, "UNCLASSIFIED",
                             f"install-only key not declared correct-by-schema: {fmt(h_val)}"))
    return records, None


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--repo", default=".",
                    help="meept repo checkout (default: cwd)")
    ap.add_argument("--home", default=os.environ.get("MEEPT_HOME") or
                    os.path.expanduser("~/.meept"),
                    help="installed meept home (default: $MEEPT_HOME or ~/.meept)")
    ap.add_argument("--json", action="store_true", help="JSON output")
    ap.add_argument("--strict", action="store_true",
                    help="exit 1 on ANY drift (classified or not)")
    ap.add_argument("--optional-templates", default="",
                    help="comma list of template files that MAY be absent from "
                         "the install (reported, not fatal; default: ai-infra,"
                         "epistemic_tags,pty,runtime)")
    ap.add_argument("--defect", default="",
                    help="comma list of dotted keys to treat as template-defect (CLI annotations)")
    ap.add_argument("--local-drift", default="",
                    help="comma list of dotted keys to treat as local-drift (CLI annotations)")
    args = ap.parse_args(argv)

    repo = os.path.abspath(args.repo)
    home = os.path.abspath(args.home)
    cfg_dir = os.path.join(repo, "config")
    if not os.path.isdir(cfg_dir):
        print(f"error: no config/ dir under {repo}", file=sys.stderr)
        return 2

    cli_defects = [k.strip() for k in args.defect.split(",") if k.strip()]
    cli_ld = [k.strip() for k in args.local_drift.split(",") if k.strip()]
    optional = set(
        f"{n}.json5" if not n.endswith(".json5") else n
        for n in (args.optional_templates.split(",") if args.optional_templates
                  else ["ai-infra", "epistemic_tags", "pty", "runtime"])
        if n.strip())
    optional.discard("")

    names = [f for f in os.listdir(cfg_dir) if f.endswith(".json5")]
    names = sorted(set(names) | set(TEMPLATE_KEYS))

    all_records = []
    errors = []
    skipped_optional = []
    for name in names:
        records, err = compare_file(name, repo, home)
        if err:
            if err.startswith("installed copy missing") and name in optional:
                skipped_optional.append(name)
                continue
            errors.append(err)
            continue
        recs = []
        for r in records:
            if r.kind == "UNCLASSIFIED":
                if value_matches(cli_ld, r.key):
                    r.kind, r.detail = "local-drift", r.detail + " (CLI annotation)"
                elif value_matches(cli_defects, r.key):
                    r.kind, r.detail = "template-defect", r.detail + " (CLI annotation)"
            recs.append(r)
        all_records.extend(recs)

    if args.json:
        print(json.dumps({
            "home": home, "repo": repo,
            "errors": errors,
            "drift": [r.__dict__ for r in all_records],
        }, indent=2))
    else:
        print(f"config drift report: repo={repo} home={home}")
        print(f"{'file':<22} {'classification':<18} key")
        print("-" * 100)
        for r in sorted(all_records, key=lambda r: (r.file_name, r.kind, r.key)):
            print(f"{r.file_name:<22} {r.kind:<18} {r.key}")
            print(f"{'':<40} {r.detail}")
        if errors:
            print("-" * 100)
            for e in errors:
                print(f"ERROR: {e}")
        if skipped_optional:
            print("optional templates absent from install (not fatal): "
                  + ", ".join(sorted(skipped_optional)))
        counts = {}
        for r in all_records:
            counts[r.kind] = counts.get(r.kind, 0) + 1
        print("-" * 100)
        print("summary:", ", ".join(f"{k}={v}" for k, v in sorted(counts.items()))
              or "no drift")

    unclassified = sum(1 for r in all_records if r.kind == "UNCLASSIFIED")
    if errors:
        return 2
    if unclassified:
        return 1
    if args.strict and all_records:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
