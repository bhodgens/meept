package voiceframe

// The voiceframe package must stay dependency-free (stdlib only): the
// daemon side (internal/voice) and the child side (internal/voicechild)
// share this codec, and neither may drag the other's world in through it.
// jargo is NEVER imported here (master.md coding conventions, gate test T9).
//
// The companion scan in internal/voice (import_test.go, activated at leaf
// completion) covers internal/voice; this one covers internal/voiceframe.
