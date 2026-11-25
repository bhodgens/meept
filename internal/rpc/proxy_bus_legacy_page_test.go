package rpc

import (
	"testing"
	"time"
)

// TestBusPollLegacyFirstPageNotDoubleCounted pins the accounting hole
// that failed the CI race tier twice on 1be09f21
// (TestBusPollStressFastProducerSlowPoller: delivered=200 dropped=400
// published=500 - a deterministic OVERCOUNT). Root cause: polledSeq,
// the consumed watermark feeding trim accounting, advanced only on
// since_seq>0 polls. A cursor-0 (legacy) page DELIVERS the ring without
// marking those records consumed, so every later trim of a delivered
// record counted it again in `dropped`.
//
// Deterministic script (ring cap 100, no load timing): publish 60,
// deliver them via cursor-0 pages, publish 120 more (total 180 - the
// ring must trim 80 records), then settle with cursor-0 polls until the
// assignment cursor and the full ring window are observed. The binding
// contract is the collection identity: unique delivered + dropped ==
// published. Under the bug, every delivered-then-trimmed record lands
// in BOTH buckets, so delivered+dropped > published (CI: 200+400 > 500).
func TestBusPollLegacyFirstPageNotDoubleCounted(t *testing.T) {
	f := newBusStreamFixture(t)
	subID := f.subscribe("legacy-overflow")

	const total = 180

	for i := range 60 {
		f.publish("legacy-overflow", "x")
		_ = i
	}
	f.waitBuffered(subID, 60)

	delivered := map[int64]bool{}
	acc := func(resp pollResp) {
		for _, e := range resp.Events {
			delivered[e.Seq] = true
		}
	}
	acc(f.poll(subID, 0)) // legacy delivery of seqs 1..60

	for range total - 60 {
		f.publish("legacy-overflow", "x")
	}

	// Settle: keep taking cursor-0 pages until every published record
	// is accounted (delivered or dropped) and the ring has slid to its
	// final 100-window.
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp := f.poll(subID, 0)
		acc(resp)
		if resp.NextSeq >= total && int64(len(delivered))+resp.Dropped >= total {
			if int64(len(delivered))+resp.Dropped != total {
				t.Fatalf("OVERCOUNT: delivered=%d dropped=%d published=%d (delivered-then-trimmed records double-bucketed)",
					len(delivered), resp.Dropped, total)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("never settled: delivered=%d dropped=%d next_seq=%d",
				len(delivered), resp.Dropped, resp.NextSeq)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
