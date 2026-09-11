package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
)

// tfidfVeto is the Door-1 agreement checker: a char n-gram TF-IDF
// logistic classifier trained on the gold corpus
// (scripts/build_tfidf_veto.py). When both it and the centroid head
// pick the same intent, Door 1 routes; disagreement falls to the LLM
// chain. Measured on the adjudicated gold replay: 87.35% system
// accuracy vs 84.56% without the veto — above the 86.8% chain-only
// floor (tools/classifier-eval/results/m4-gold-acceptance.md).
//
// Cost: ~0.4ms/message, ~2MB model, no network. Everything here is
// stdlib — the training script is python (sklearn-equivalent math
// ported to stdlib so inference needs nothing new).
type tfidfVeto struct {
	vocab      map[string]int
	idf        []float64
	classes    []string
	coef       [][]float64
	intercept  []float64
	ngramLo    int
	ngramHi    int
	charWB     bool
	builtAt    string
	trainDocs  int
	trainAcc   float64
	loaded     bool
}

// loadTfidfVeto reads the model JSON; missing file returns (nil, nil)
// so the veto is silently disabled when not built — Door 1 then behaves
// exactly as before (design principle: prefilter can only skip work).
func loadTfidfVeto(path string) (*tfidfVeto, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // not built: veto disabled
		}
		return nil, fmt.Errorf("read tfidf veto model: %w", err)
	}
	var m struct {
		Vocab      map[string]int   `json:"vocab"`
		IDF        []float64        `json:"idf"`
		Classes    []string         `json:"classes"`
		Coef       [][]float64      `json:"coef"`
		Intercept  []float64        `json:"intercept"`
		NgramRange []int            `json:"ngram_range"`
		CharWB     bool             `json:"char_wb"`
		BuiltAt    string           `json:"built_at"`
		TrainDocs  int              `json:"train_docs"`
		TrainAcc   float64          `json:"train_accuracy"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse tfidf veto model: %w", err)
	}
	if len(m.Vocab) == 0 || len(m.Classes) == 0 || len(m.Coef) != len(m.Classes) {
		return nil, fmt.Errorf("tfidf veto model incomplete")
	}
	lo, hi := 2, 4
	if len(m.NgramRange) == 2 {
		lo, hi = m.NgramRange[0], m.NgramRange[1]
	}
	return &tfidfVeto{
		vocab:     m.Vocab,
		idf:       m.IDF,
		classes:   m.Classes,
		coef:      m.Coef,
		intercept: m.Intercept,
		ngramLo:   lo,
		ngramHi:   hi,
		charWB:    m.CharWB,
		builtAt:   m.BuiltAt,
		trainDocs: m.TrainDocs,
		trainAcc:  m.TrainAcc,
		loaded:    true,
	}, nil
}

// predict returns (bestClass, confidence).
func (t *tfidfVeto) predict(text string) (string, float64) {
	vec := t.vectorize(text)
	scores := make([]float64, len(t.classes))
	for c := range t.classes {
		s := t.intercept[c]
		row := t.coef[c]
		for j, xv := range vec {
			if xv != 0 {
				s += row[j] * xv
			}
		}
		scores[c] = s
	}
	best := 0
	for c := 1; c < len(scores); c++ {
		if scores[c] > scores[best] {
			best = c
		}
	}
	// softmax for a confidence readout
	maxs := scores[best]
	sum := 0.0
	for _, s := range scores {
		sum += math.Exp(s - maxs)
	}
	conf := 1.0 / sum // exp(0)/(sum) for the max element
	return t.classes[best], conf
}

// vectorize: lowercase, optional word-boundary padding, char n-grams,
// idf-weighted. Matches the training script's feature extraction; the
// sublinear-tf log compression is skipped at inference (documented
// trade-off: n>=2 grams rarely repeat in short prompts, and the veto
// only needs argmax stability, not calibrated probabilities).
func (t *tfidfVeto) vectorize(text string) []float64 {
	text = strings.ToLower(text)
	if t.charWB {
		text = " " + text + " "
	}
	vec := make([]float64, len(t.vocab))
	for n := t.ngramLo; n <= t.ngramHi; n++ {
		for i := 0; i+n <= len(text); i++ {
			g := text[i : i+n]
			if j, ok := t.vocab[g]; ok {
				vec[j] += t.idf[j]
			}
		}
	}
	return vec
}

// agrees reports whether the veto model's top intent matches the
// centroid head's pick. Unknown intent (not in training classes) =
// disagreement.
func (t *tfidfVeto) agrees(text, centroidIntent string) bool {
	if t == nil || !t.loaded {
		return true // veto disabled: never block Door 1
	}
	pred, _ := t.predict(text)
	return pred == centroidIntent
}
