"""One-shot generator for the prompt-leak fixtures (leaf 07).

Ports the CASES shapes from uptractor
phone/core/sidecar-cloud/eval/prompt_leak.py (branch
fix/coordinator-missed-hangup-watchdog) to committed WAV files:
digital silence, line hiss, and synthetic hold music. Run once; the
outputs are committed, so the lane never needs this script at runtime.
"""
import math
import random
import struct
import wave

OUT = ("/Users/caimlas/git/meept/.hermes/worktrees/"
       "20261110-uptractor-ports/testdata/voice-eval")
SR = 16000


def write(name, samples):
    w = wave.open(f"{OUT}/{name}", "w")
    w.setnchannels(1)
    w.setsampwidth(2)
    w.setframerate(SR)
    w.writeframes(b"".join(
        struct.pack("<h", max(-32768, min(32767, int(s)))) for s in samples))
    w.close()


rng = random.Random(5)  # prompt_leak.py rng seed

# prompt_leak.py: "digital silence 10s" -> f32(np.zeros(SR*10))
write("leak-silence.wav", [0.0] * (SR * 6))

# prompt_leak.py: "hiss 600 10s" -> hiss(10, 600) = rng.normal(0, 600)
write("leak-hiss.wav", [rng.gauss(0, 600) for _ in range(SR * 6)])

# prompt_leak.py music(): chords 262/330/392/523 Hz, 0.3 Hz amp LFO,
# r=1500 ("hold music 12s"), rescaled to 6 s here.
write("leak-music.wav", [
    sum(math.sin(2 * math.pi * f * t / SR)
        * (0.5 + 0.5 * math.sin(2 * math.pi * 0.3 * t / SR))
        for f in (262, 330, 392, 523)) / 4 * 1500
    for t in range(SR * 6)])

print("written 3 leak fixtures")
