# READ-ONLY Audit: Native Rumble WS STT Streaming (tree 20261119-rumble-native-streaming)

**Date:** 2026-11-19
**Scope:** commits e335ea15, f5e384a2, 26790fa9, 7a5da96e
**Files:** internal/voicechild/providers/stt_rumble.go, internal/voicechild/providers/pseudostream.go, internal/voicechild/providers/stt_deepgram.go, internal/config/schema.go, config/meept.json5
**Audit type:** READ-ONLY — no source edits
**Report file:** this document

## Checked clean (no defect found)

- config/meept.json5 knob (RUMBLE_STT_STREAMING_URL)
- internal/config/schema.go 13-line change
- internal/voicechild/providers/stt_deepgram.go 25-line change (deepgram endpoint override)
- internal/voicechild/providers/pseudostream.go bypass logic (unset = byte-identical)
