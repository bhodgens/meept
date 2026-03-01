package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestShouldDispatchAsync(t *testing.T) {
	d := &Dispatcher{}

	tests := []struct {
		name   string
		result *DispatchResult
		want   bool
	}{
		{
			name:   "nil result",
			result: nil,
			want:   false,
		},
		{
			name:   "nil intent",
			result: &DispatchResult{},
			want:   false,
		},
		{
			name: "skill response (always sync)",
			result: &DispatchResult{
				Intent:   &Intent{Type: "code"},
				Response: "skill handled this",
			},
			want: false,
		},
		{
			name: "code intent",
			result: &DispatchResult{
				Intent: &Intent{Type: "code"},
			},
			want: true,
		},
		{
			name: "debug intent",
			result: &DispatchResult{
				Intent: &Intent{Type: "debug"},
			},
			want: true,
		},
		{
			name: "plan intent",
			result: &DispatchResult{
				Intent: &Intent{Type: "plan"},
			},
			want: true,
		},
		{
			name: "schedule intent",
			result: &DispatchResult{
				Intent: &Intent{Type: "schedule"},
			},
			want: true,
		},
		{
			name: "chat intent (sync)",
			result: &DispatchResult{
				Intent: &Intent{Type: "chat"},
			},
			want: false,
		},
		{
			name: "requires planning flag",
			result: &DispatchResult{
				Intent: &Intent{Type: "unknown", RequiresPlanning: true},
			},
			want: true,
		},
		{
			name: "code intent with task",
			result: &DispatchResult{
				Intent: &Intent{Type: "code"},
				Task:   task.NewTask("test", "test"),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.ShouldDispatchAsync(tt.result)
			if got != tt.want {
				t.Errorf("ShouldDispatchAsync() = %v, want %v", got, tt.want)
			}
		})
	}
}
