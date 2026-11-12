# Voice eval lane (leaf 07, master "Eval harness" matrix row). This target
# is the 00T gate stub: it exists so `make test-voice-eval` is exercised
# NOW (the gate must fail RED — the lane's Go package does not exist until
# leaf 07 lands) and so leaf 07 fills the recipe in place, keeping the
# target name frozen. Leaf 07 replaces the echo with the real lane:
#
#	test-voice-eval:
#		go test -p 2 ./internal/voice/eval/ -v
#
# Fixtures live under testdata/voice-eval/ (demo-utterance.wav ported from
# uptractor phone/core/sidecar/fixtures/, plus the prompt-leak and clean
# expected.json cases). Thresholds and needles are ported verbatim from
# phone/core/sidecar-cloud/eval/{accuracy.py,prompt_leak.py}.
test-voice-eval:
	@echo "RED GATE: test-voice-eval lane not implemented yet (leaf 07); fixtures staged under testdata/voice-eval/"
	@exit 1
