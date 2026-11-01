package gatecapture

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// ExportDPO writes one DPO row per record (Contract C5, pair_kind "record"),
// then appends the derived near-miss contrastive pairs (pair_kind
// "near_miss"), as JSONL. The field mapping is the FROZEN C5 shape:
//
//	{"prompt": <ContextBlob>, "chosen": <human decision text>,
//	 "rejected": <model contrary position>, "pair_kind": ..., "margin": 1.0}
//
// The shape deliberately matches the existing shadow DPO exporter's row
// format (internal/shadow/exporter.go exportDPO: the same
// prompt/chosen/rejected/domain/task_type/margin key family with pair_kind
// added) - no fork of the format. opts.Chokepoint and opts.Since filter the
// record set; Margin is the constant 1.0 (a human verdict is a hard label).
func (s *Store) ExportDPO(ctx context.Context, w io.Writer, opts ListOptions) (int, error) {
	if s == nil || w == nil {
		return 0, nil
	}
	records, err := s.ListRecords(ctx, opts)
	if err != nil {
		return 0, err
	}

	bw := bufio.NewWriter(w)
	count := 0
	for _, rec := range records {
		row := recordRow(rec, pairKindRecord)
		if err := writeRow(bw, row); err != nil {
			return count, err
		}
		count++
	}
	for _, row := range deriveNearMissPairs(records) {
		if err := writeRow(bw, row); err != nil {
			return count, err
		}
		count++
	}
	if err := bw.Flush(); err != nil {
		return count, fmt.Errorf("gatecapture: flush export: %w", err)
	}
	return count, nil
}

// writeRow marshals and writes one JSONL row.
func writeRow(w *bufio.Writer, row exportRow) error {
	data, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("gatecapture: marshal export row: %w", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("gatecapture: write export row: %w", err)
	}
	return nil
}

// NearMissOptions carries the near-miss knobs for DeriveNearMissPairs (the
// exported deterministic entry point used by tests and the eval leaf).
type NearMissOptions struct {
	// Chokepoint / Since filter the input record set.
	Chokepoint string
	Since      *time.Time
}

// DeriveNearMissPairs lists records under opts and returns the derived
// near-miss pairs. Deterministic over the same input set.
func (s *Store) DeriveNearMissPairs(ctx context.Context, opts NearMissOptions) ([]exportRow, error) {
	records, err := s.ListRecords(ctx, ListOptions{
		Chokepoint: opts.Chokepoint,
		Since:      opts.Since,
	})
	if err != nil {
		return nil, err
	}
	return deriveNearMissPairs(records), nil
}
