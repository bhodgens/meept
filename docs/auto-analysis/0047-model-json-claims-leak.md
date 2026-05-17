# LLM Response Includes Raw JSON Claims Block in Output
**Date**: 2026-05-15
**Phase**: 1
**Severity**: low
**Component**: internal/llm/client.go
**Evaluation Dimension**: communication, helpfulness

## Description
When using the `~/go/bin/` daemon (older binary with model "n/a"), LLM responses include a raw JSON "claims" block at the end of the response text. This is internal metadata that should not be exposed to users.

## Reproduction
```bash
# With the older daemon binary that has model n/a
~/git/meept/bin/meept chat "what color is the sky?"
```

## Evidence
```
The sky typically appears blue during the day because of Rayleigh scattering...

```json
{
  "claims": ["Answered the question about the color of the sky"],
  "evidence": []
}
```
```

## Root Cause
The LLM prompt includes instructions for structured output (claims/evidence), but the response parser does not strip the JSON block before returning it to the user. This likely happens when:
1. The "Strip report" logic doesn't match the claims format
2. The model outputs the claims block in a different format than expected by the parser

## Impact on Platform Quality
- Users see internal metadata in responses
- Reduces trust in the system's polish and reliability
- Confusing for non-technical users

## Proposed Fix
Add a post-processing step that strips JSON blocks from the response before returning to the user. The claims/evidence structure should be parsed and handled internally, not passed through to the display layer.

## Classification
[ ] Harness bug  [x] Model quality issue  [ ] Communication issue  [ ] Efficiency issue  [ ] Design gap  [ ] Both
