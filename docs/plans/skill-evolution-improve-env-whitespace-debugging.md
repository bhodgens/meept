# Plan: Skill evolution: improve env-whitespace-debugging

## Meta

- plan_id: plan-20260831231916-0003
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is 0.0 with 3 negative and 0 positive outcomes. The skill likely lacks specific diagnostic steps and concrete remediation patterns for env whitespace issues, causing agents to apply incorrect or incomplete fixes.

Candidate content:
# env-whitespace-debugging

## Purpose
Diagnose and fix whitespace-related issues in environment files (.env, .env.local, .env.production, etc.) that cause runtime failures, parsing errors, or silent misconfiguration.

## When to Use
- Application fails to start with unclear env-related errors
- Variables appear unset despite being defined
- Parsing errors from dotenv/envy/config-loader libraries
- Quoting issues in shell scripts or Dockerfiles referencing env vars
- CI/CD pipeline failures around secret injection

## Detection Patterns

### Common Whitespace Issues
1. **Leading/trailing spaces** around values: `KEY= value ` → parsed as literal ` value `
2. **Embedded spaces in unquoted values**: `DB_URL=postgres://host mydb` → broken connection string
3. **Tabs mixed with spaces**: invisible characters causing parser failures
4. **Missing quotes around values with special chars**: `SECRET=abc def!@#` → shell interpretation
5. **BOM or zero-width characters**: especially from Windows editors
6. **Trailing comments without separator**: `KEY=value #comment` (no space before #)
7. **Empty lines consumed as values**: blank lines between assignments

## Remediation Steps

### Step 1: Inspect Raw File Contents
```
# Reveal hidden whitespace
xxd .env | head -50
# or
cat -A .env
# or
python3 -c "import sys; print(repr(open('.env').read()))"
```

### Step 2: Validate Key-Value Parsing
```bash
# Test dotenv parsing if available
node -e "require('dotenv').config(); console.log(process.env)"
# or
python3 -c "import dotenv; print(dotenv.dotenv_values('.env'))"
```

### Step 3: Apply Fixes
- Remove leading/trailing whitespace from values
- Wrap values containing spaces or special characters in double quotes
- Replace tabs with spaces (or remove entirely)
- Strip BOM: `iconv -f UTF-8 -t UTF-8//IGNORE .env > .env.clean && mv .env.clean .env`
- Add space before `#` for inline comments
- Ensure each KEY=VALUE pair is on its own line

### Step 4: Post-Fix Verification
```
# Re-run parsing validation
# Compare against known-good values
diff <(grep -v '^#' .env | sort) <(grep -v '^#' .env.good | sort)
```

## Output Format
```json
{
  "issues_found": [
    {"line": 3, "type": "trailing_space", "raw": "DB_HOST=localhost ", "fixed": "DB_HOST=localhost"}
  ],
  "files_modified": [".env"],
  "verification": "pass|fail",
  "residual_warnings": []
}
```

## Prevention
- Configure editor to strip trailing whitespace on save
- Use `.editorconfig` with `trim_trailing_whitespace = true`
- Add env file linting to CI: `shellcheck` for bash, `dotenv-linter` for .env files
- Use quoted values consistently: `KEY="value with spaces"`
- Never commit .env files with sensitive data; use templates (.env.example)


## Notes

