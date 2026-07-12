package learning

import (
	"testing"
	"time"
)

func TestDomainsReadyForTrain(t *testing.T) {
	t.Parallel()

	meta := &LearningMetadata{
		DomainStats: map[string]DomainStat{
			"code":      {ExampleCount: 600},
			"debugging": {ExampleCount: 100},
			"security":  {ExampleCount: 500},
		},
		LastAutoTrain: map[string]AutoTrainRecord{
			"code": {
				ExampleCount: 600,
				Status:       "completed",
				TrainedAt:    time.Now().UTC(),
			},
		},
	}

	ready := DomainsReadyForTrain(meta, 500)
	if len(ready) != 1 || ready[0] != "security" {
		t.Fatalf("ready = %v, want [security]", ready)
	}

	meta.DomainStats["code"] = DomainStat{ExampleCount: 700}
	ready = DomainsReadyForTrain(meta, 500)
	if len(ready) != 2 {
		t.Fatalf("ready after growth = %v, want 2 domains", ready)
	}

	if DomainsReadyForTrain(nil, 500) != nil {
		t.Error("nil meta should return nil")
	}
	if DomainsReadyForTrain(meta, 0) != nil {
		t.Error("threshold 0 should return nil")
	}
}

func TestEnqueueListClearPendingAutoTrain(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	if err := EnqueueAutoTrain(tmp, PendingAutoTrain{
		Domain:       "code",
		Model:        "lfm2.5-8b",
		ExampleCount: 500,
	}); err != nil {
		t.Fatal(err)
	}
	if err := EnqueueAutoTrain(tmp, PendingAutoTrain{
		Domain:       "code",
		Model:        "lfm2.5-8b",
		ExampleCount: 500,
	}); err != nil {
		t.Fatal(err)
	}
	if err := EnqueueAutoTrain(tmp, PendingAutoTrain{
		Domain:       "debugging",
		Model:        "lfm2.5-8b",
		ExampleCount: 600,
	}); err != nil {
		t.Fatal(err)
	}

	list, err := ListPendingAutoTrain(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}

	if err := ClearPendingAutoTrain(tmp, "code", "lfm2.5-8b"); err != nil {
		t.Fatal(err)
	}
	list, err = ListPendingAutoTrain(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Domain != "debugging" {
		t.Fatalf("after clear = %+v, want only debugging", list)
	}

	if err := MarkAutoTrainStarted(tmp, "debugging", "lfm2.5-8b", 600); err != nil {
		t.Fatal(err)
	}
	if err := MarkAutoTrainCompleted(tmp, "debugging", "lfm2.5-8b", 600); err != nil {
		t.Fatal(err)
	}
	meta, err := LoadMetadata(tmp)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := meta.LastAutoTrain["debugging"]
	if !ok || rec.Status != "completed" {
		t.Fatalf("LastAutoTrain = %+v", meta.LastAutoTrain)
	}
}
