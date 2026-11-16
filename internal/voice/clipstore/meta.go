package clipstore

import "time"

// Meta is the JSON sidecar stored at <clipKey>.json per Contract C2:
//
//	{key, text, vendor, model, voice, sample_rate, bytes, created_at}
//
// The json field names are byte-exact — the child (leaf 03) writes and
// both daemon and child (leaves 02/04) read these files; the bytes field
// is the cache-hit validation length, sample_rate the vendor's native
// output rate (24000 for qwen3; the playback path resamples from it).
type Meta struct {
	Key        string    `json:"key"`
	Text       string    `json:"text"`
	Vendor     string    `json:"vendor"`
	Model      string    `json:"model"`
	Voice      string    `json:"voice"`
	SampleRate int       `json:"sample_rate"`
	Bytes      int64     `json:"bytes"`
	CreatedAt  time.Time `json:"created_at"`
}
