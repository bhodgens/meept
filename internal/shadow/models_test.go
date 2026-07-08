package shadow

import "testing"

func TestNewPreferencePair_PreservesDomainAndTaskType(t *testing.T) {
	record := &ShadowRecord{
		Domain:         DomainCode,
		TaskType:       TaskTypeReasoning,
		StudentContent: "student",
		TeacherContent: "teacher",
		StudentModel:   "qwen2.5:7b",
		TeacherModel:   "claude-opus-4",
	}
	pair := NewPreferencePair(record, 0.3, 0.9) // teacher wins
	if pair.Domain != DomainCode {
		t.Errorf("expected Domain=code, got %v", pair.Domain)
	}
	if pair.TaskType != TaskTypeReasoning {
		t.Errorf("expected TaskType=reasoning, got %v", pair.TaskType)
	}
	if pair.Margin < 0.59 || pair.Margin > 0.61 {
		t.Errorf("expected margin ~0.6, got %v", pair.Margin)
	}
}

func TestNewPreferencePair_PreservesRoutingPath(t *testing.T) {
	record := &ShadowRecord{
		Domain:         DomainCode,
		TaskType:       TaskTypeReasoning,
		RoutingPath:    "skill:coding",
		StudentContent: "student",
		TeacherContent: "teacher",
		StudentModel:   "qwen2.5:7b",
		TeacherModel:   "claude-opus-4",
	}
	pair := NewPreferencePair(record, 0.3, 0.9) // teacher wins
	if pair.RoutingPath != "skill:coding" {
		t.Errorf("expected RoutingPath=skill:coding, got %q", pair.RoutingPath)
	}
}
