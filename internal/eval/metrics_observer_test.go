package eval

import "testing"

func TestMetricsObserver_NilSafe(t *testing.T) {
	var m *MetricsObserver
	m.RecordPassK("task", true) // must not panic
	m.RecordOracle("oracle", false)
	if _, _, _, _ = m.Counters(); false {
		t.Fatal("unreachable")
	}
}

func TestMetricsObserver_RecordsAndCounts(t *testing.T) {
	var recorded []string
	m := NewMetricsObserver(func(name string, value float64, tags map[string]string) {
		recorded = append(recorded, name+"|"+tags["passed"])
	})

	m.RecordPassK("taskA", true)
	m.RecordPassK("taskB", false)
	m.RecordOracle("go-test", true)
	m.RecordOracle("go-test", false)

	passK, passKPassed, oracle, oraclePassed := m.Counters()
	if passK != 2 || passKPassed != 1 {
		t.Fatalf("passK counters = (%d, %d), want (2, 1)", passK, passKPassed)
	}
	if oracle != 2 || oraclePassed != 1 {
		t.Fatalf("oracle counters = (%d, %d), want (2, 1)", oracle, oraclePassed)
	}
	if len(recorded) != 4 {
		t.Fatalf("recorded %d sink calls, want 4: %v", len(recorded), recorded)
	}
	if recorded[0] != "eval_pass_k_total|true" || recorded[1] != "eval_pass_k_total|false" {
		t.Fatalf("passK sink rows wrong: %v", recorded[:2])
	}
	if recorded[2] != "eval_oracle_runs_total|true" {
		t.Fatalf("oracle sink rows wrong: %v", recorded[2:])
	}
}

func TestMetricsObserver_NilSinkStillCounts(t *testing.T) {
	m := NewMetricsObserver(nil)
	m.RecordPassK("task", true)
	if _, passKPassed, _, _ := m.Counters(); passKPassed != 1 {
		t.Fatalf("passKPassed = %d, want 1", passKPassed)
	}
}
