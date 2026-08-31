# Plan: Skill evolution: improve env-whitespace-debugging

## Meta

- plan_id: plan-20260831224633-0037
- created: 2026-08-31
- status: planning

## Summary

Zero effectiveness with 3 negative outcomes indicates the skill is misfireing. The current version is too narrow, only addressing whitespace in file content. It needs broader detection (trailing spaces, mixed indentation, invisible Unicode whitespace), proper root-cause classification, and clearer remediation guidance.

Candidate content:
# env-whitespace-debugging

## Purpose
Detect and clean invisible whitespace characters that cause hidden failures in configs, scripts, diffs, CI output, and serialized data.

## When to Use
- Lint/format tools pass but behavior is inconsistent across environments
- Config parsers reject values that look correct
- Git diffs show changes in apparently identical files
- CI/CD failures occur only on specific runners
- Diff tools report unchanged files as different
- String comparisons fail for visually identical text
- Encoded payloads (base64, JSON, YAML) contain subtle differences

## Detection Patterns

### Check for trailing whitespace
```bash
# Find trailing spaces/tabs in text files
grep -nP '[ \t]+$' <file>
grep -rnP '[ \t]+$' --include='*.yaml' --include='*.yml' --include='*.json' --include='*.env' --include='*.sh' --include='*.py' .
```

### Check for mixed indentation
```bash
# Detect tabs vs spaces inconsistency
find . -type f \( -name '*.yaml' -o -name '*.yml' -o -name '*.json' \) -exec sh -c 'if grep -qP "\t" "$1"; then echo "$1: contains tabs"; fi' _ {} \;
```

### Check for non-printable/invisible Unicode whitespace
```bash
# BOM, zero-width spaces, thin spaces, etc.
grep -Pn '[\x00-\x08\x0B\x0C\x0E-\x1F\x7F\uFEFF\u200B\u200C\u200D\u202F\u205F\u3000]' <file>
# Or use cat -A / vi to reveal invisible chars
```

### Check line ending consistency
```bash
# Detect mixed CRLF/LF
git diff --check
git diff --stat | grep -i crlf
# Or per-file:
cat -vet <file> | grep -E '\^M|\$'
```

### Check encoded payload whitespace
```bash
# In base64/JSON/YAML, check if surrounding whitespace differs
xxd <file> | head
python3 -c "import json; json.loads(open('<file>').read())"  # Python is strict about trailing commas
```

## Remediation Steps

1. **Identify the scope**: Determine which files, environment variables, or config values are affected.
2. **Normalize line endings**:
   ```bash
   sed -i 's/\r$//' <file>   # Remove CR, keep LF
   dos2unix <file>             # If dos2unix is available
   ```
3. **Remove trailing whitespace**:
   ```bash
   sed -i -E 's/[[:space:]]+$//' <file>    # POSIX sed
   # Or with perl:
   perl -pi -e 's/\s+$//' <file>
   ```
4. **Normalize indentation** (pick one style):
   ```bash
   # Convert tabs to spaces (4-space indent)
   expand -t 4 <file> > tmp && mv tmp <file>
   # Or spaces to tabs
   unexpand -t 4 --first-only <file> > tmp && mv tmp <file>
   ```
5. **Strip invisible Unicode whitespace**:
   ```bash
   python3 -c "
import sys
with open('<file>', 'r', encoding='utf-8-sig') as f:
    content = f.read()
clean = content.replace('\u200b','').replace('\u200c','').replace('\u200d','').replace('\ufeff','').replace('\u202f','').replace('\u3000',' ')
with open('<file>', 'w', encoding='utf-8') as f:
    f.write(clean)
"
   ```
6. **Re-run the failing check** to confirm resolution.

## Common Pitfalls
- **Editor auto-formatting**: Some editors insert zero-width spaces or alter line endings. Disable auto-format on save for sensitive files.
- **Cross-platform transfers**: Files moved between Windows/Linux/macOS often accumulate mixed line endings. Always normalize after transfer.
- **Environment variable strings**: Values passed via CI secrets or Docker ENV may contain leading/trailing spaces. Use `printf '%s' "$VAR"` to inspect.
- **Base64 whitespace**: Some base64 decoders reject embedded newlines. Strip whitespace before decoding if needed: `tr -d ' \n' < encoded.txt`.
- **Config parser tolerance**: Python's `json.load()` and `yaml.safe_load()` may silently accept extra whitespace while strict parsers reject it. Check which parser your toolchain uses.

## Verification
After remediation, run:
```bash
# Confirm no remaining issues
grep -nP '[ \t]+$' <file>           # No trailing whitespace
grep -P '[\uFEFF\u200B-\u200D]' <file>  # No invisible Unicode
file <file>                          # Confirm encoding/line ending
diff <(cat -vet <old>) <(cat -vet <new>)  # Compare with visible markers
```

## Notes

