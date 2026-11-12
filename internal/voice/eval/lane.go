package eval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/voicechild/providers"
)

// WeightsDirOverrideEnv lets a test point the lane at a scratch weights
// dir (the loud-skip test renames a copy away inside t.TempDir). Empty in
// production: the lane resolves meept's default whisper model path
// (~/.meept/models/ggml-base.en.bin, the internal/stt convention).
const WeightsDirOverrideEnv = "MEEPT_VOICE_EVAL_MODEL_DIR"

// whisperBin is the whisper.cpp CLI the providers whisper adapter shells
// out to (providers.defaultWhisperBin; duplicated as a literal so this
// package does not reach for an unexported name).
const whisperBin = "whisper-cli"

// defaultModelRelPath is meept's default whisper model location relative
// to the user home.
var defaultModelRelPath = filepath.Join(".meept", "models", "ggml-base.en.bin")

// fixtureWAVSampleRate is the rate the adapter sees: fixtures are 24 kHz
// (piper/operator) and the providers whisper adapter resamples on write
// via its resampling reader; SampleRate on the config declares the
// incoming PCM rate.
const fixtureWAVSampleRate = 24000

// caseTimeout bounds one fixture's STT run.
const caseTimeout = 120 * time.Second

// errWeightsMissing is the loud-skip sentinel: weights absent is NOT a
// lane failure, but it must never pass silently.
var errWeightsMissing = errors.New("whisper weights absent")

// weightsStatus resolves the lane's weights. Order: the env override dir
// (test seam), then $HOME/<defaultModelRelPath>. Non-nil error = missing;
// the error message always NAMES the missing path.
func weightsStatus() (string, error) {
	if dir := os.Getenv(WeightsDirOverrideEnv); dir != "" {
		p := filepath.Join(dir, "ggml-base.en.bin")
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("%w: %s", errWeightsMissing, p)
		}
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%w: resolve home: %v", errWeightsMissing, err)
	}
	p := filepath.Join(home, defaultModelRelPath)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("%w: %s", errWeightsMissing, p)
	}
	return p, nil
}

// repoRootFrom walks up from start until the directory containing
// testdata/voice-eval is found (go test runs the package dir; the lane
// must work from any depth and from `go run` invocations too).
func repoRootFrom(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("voice eval: resolve %s: %w", start, err)
	}
	dir := abs
	for i := 0; i < 12; i++ {
		if fi, err := os.Stat(filepath.Join(dir, "testdata", "voice-eval")); err == nil && fi.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("voice eval: testdata/voice-eval not found above %s", start)
}

// warnLoud is the loud-skip printer. It ALWAYS names the missing path;
// the lane returns skip=true (exit 0) — never a silent pass.
func warnLoud(missing string) {
	fmt.Printf("WARNING: test-voice-eval SKIPPED LOUDLY: whisper weights not found: %s\n", missing)
	fmt.Printf("WARNING: install the model (whisper.cpp ggml-base.en.bin -> ~/.meept/models/) and re-run; the lane did NOT evaluate anything\n")
}

// transcribeFixture runs one committed WAV through the REAL local STT
// path: providers.NewSTT (vendor "whisper") -> WhisperSTT.Stream with
// the file decoded to s16le PCM chunks. No network; whisper.cpp is a
// local subprocess. Returns the concatenated final transcripts.
// NOTE: each fixture gets a FRESH adapter — WhisperSTT buffers
// utterances per Stream, and fixtures are independent utterances.
func transcribeFixture(ctx context.Context, modelPath, wavPath string) (string, error) {
	stt, err := providers.NewSTT(providers.ProviderConfig{
		Vendor:     "whisper",
		Model:      modelPath,
		SampleRate: fixtureWAVSampleRate,
		Language:   "en",
	}, "" /* keyless local vendor */)
	if err != nil {
		return "", fmt.Errorf("build whisper STT: %w", err)
	}
	pcmBytes, err := readWAVMono16(wavPath)
	if err != nil {
		return "", err
	}
	pcm := make(chan []byte, 4)
	out, err := stt.Stream(ctx, pcm)
	if err != nil {
		return "", fmt.Errorf("open stt stream: %w", err)
	}
	go func() {
		defer close(pcm)
		// 100 ms chunks of s16le mono at 24 kHz (4800 bytes). Closing
		// pcm flushes the adapter's trailing-utterance path — the
		// fixture has no trailing silence for the energy gate to ride.
		const chunk = 4800
		for i := 0; i < len(pcmBytes); i += chunk {
			end := i + chunk
			if end > len(pcmBytes) {
				end = len(pcmBytes)
			}
			pcm <- pcmBytes[i:end]
		}
	}()
	var parts []string
	deadline := time.After(caseTimeout)
	for {
		select {
		case t, ok := <-out:
			if !ok {
				return joinTranscripts(parts), nil
			}
			if t.Final && t.Text != "" {
				parts = append(parts, t.Text)
			}
		case <-deadline:
			return "", fmt.Errorf("stt stream timeout on %s", wavPath)
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func joinTranscripts(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " "
		}
		out += p
	}
	return out
}

// LaneReport is the lane outcome: Skipped=true means loud-skip (exit 0
// policy); Failures counts failed cases; FailReasons names them.
type LaneReport struct {
	Skipped     bool
	Failures    int
	FailReasons []string
}

// RunLane executes the persistent eval from repoRoot: loads the case
// set, checks weights (loud skip), runs every accuracy fixture and every
// non-speech leak fixture through the real whisper adapter, scores, and
// prints a per-case verdict.
func RunLane(repoRoot string) (*LaneReport, error) {
	rep := &LaneReport{}
	fixtureDir := filepath.Join(repoRoot, "testdata", "voice-eval")
	cs, err := LoadCaseSet(fixtureDir)
	if err != nil {
		return rep, err
	}
	modelPath, werr := weightsStatus()
	if werr != nil {
		warnLoud(werr.Error())
		rep.Skipped = true
		return rep, nil
	}
	if _, lerr := exec.LookPath(whisperBin); lerr != nil {
		// A missing CLI is a real failure (weights present but no runner
		// is an environment break, not a skip).
		return rep, fmt.Errorf("%s not found in PATH: %w", whisperBin, lerr)
	}

	fmt.Printf("voice-eval: %d accuracy cases + %d leak cases, model %s\n",
		len(cs.Cases), len(cs.LeakCases), modelPath)

	for i := range cs.Cases {
		c := &cs.Cases[i]
		actual, terr := transcribeFixture(context.Background(), modelPath, filepath.Join(fixtureDir, c.WAV))
		if terr != nil {
			return rep, fmt.Errorf("case %s: %v", c.WAV, terr)
		}
		res := ScoreCase(c, actual)
		verdict := "PASS"
		if !res.Pass {
			verdict = "FAIL"
			rep.Failures++
			rep.FailReasons = append(rep.FailReasons, fmt.Sprintf("%s: %s", c.WAV, res.FailReason))
		}
		fmt.Printf("  accuracy %-24s WER %4.0f%%  key %d/%d  %s", c.WAV, res.WER*100, res.KeyHits, res.KeyTotal, verdict)
		if res.FailReason != "" {
			fmt.Printf(" (%s)", res.FailReason)
		}
		fmt.Printf("\n    heard: %q\n", res.Actual)
	}

	for i := range cs.LeakCases {
		c := &cs.LeakCases[i]
		actual, terr := transcribeFixture(context.Background(), modelPath, filepath.Join(fixtureDir, c.WAV))
		if terr != nil {
			return rep, fmt.Errorf("leak case %s: %v", c.WAV, terr)
		}
		res := ScoreLeakTranscript(c, actual)
		verdict := "PASS"
		if !res.Pass {
			verdict = "FAIL"
			rep.Failures++
			rep.FailReasons = append(rep.FailReasons, fmt.Sprintf("%s: %s", c.WAV, res.FailReason))
		}
		fmt.Printf("  leak     %-24s %s", c.WAV, verdict)
		if res.FailReason != "" {
			fmt.Printf(" (%s)", res.FailReason)
		}
		fmt.Printf("\n    heard: %q\n", res.Actual)
	}
	return rep, nil
}
