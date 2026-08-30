// Package scheduler — rewake.go is the timer→rewake adapter: it bridges
// scheduler job completion onto the EXISTING hook.async_rewake topic that
// the agent loop's rewake consumer is already subscribed to. It is not a
// new bus topic: the topic literal mirrors internal/agent's
// HookAsyncRewakeTopic (the agent package owns the const; importing it
// from scheduler would invert the layering the leaf spec fixes: agent must
// stay scheduler-agnostic).
//
// Session mapping: scheduler jobs carry no conversation/session identity
// (a timer fires for the daemon, not for one chat), so the published
// payload has an empty session_id. Per the loop_rewake contract, empty
// session_id is a broadcast — every loop with an armed consumer wakes.
// This is the correct behavior for timer completion: any active loop may
// need to react to a job finishing.
package scheduler

import (
	"github.com/caimlas/meept/pkg/models"
)

// rewakeSourceTimer is the RewakePayload.Source value for scheduler job
// completions. Mirrors the agent package's documented source taxonomy
// (timer|hook|user|notify_reply); kept as a literal because internal/scheduler
// must not import internal/agent (layering, not an import cycle).
const rewakeSourceTimer = "timer"

// HookAsyncRewakeTopic is the EXISTING rewake bus topic consumed by the
// agent loop (internal/agent.HookAsyncRewakeTopic). Duplicated as a
// literal to preserve package layering; a test pins both spellings equal.
const HookAsyncRewakeTopic = "hook.async_rewake"

// NotifyJobComplete publishes a hook.async_rewake signal with
// Source=timer so any armed agent loop wakes at its next iteration
// boundary. No mid-turn interrupt: the rewake consumer buffers the signal
// and injectRewakes delivers it as a system note.
//
// publish is injectable for tests (pass nil to treat it as a no-op).
// msg, if non-nil, is published to HookAsyncRewakeTopic via publish;
// the scheduler calls this from its single central job-completion site so
// every job type (agent/shell/reminder/…, cron and RunNow alike) rewakes
// the daemon.
func NotifyJobComplete(publish func(topic string, msg *models.BusMessage) int, msg *models.BusMessage) int {
	if publish == nil || msg == nil {
		return 0
	}
	return publish(HookAsyncRewakeTopic, msg)
}

// buildRewakeSignal assembles the rewake payload for one job completion.
// sessionID is intentionally empty: scheduler jobs have no session
// identity, and per the loop_rewake contract empty session_id means
// broadcast to every armed loop.
func buildRewakeSignal(jobID, jobName string) (*models.BusMessage, error) {
	payload := map[string]any{
		"session_id":      "",
		"source":          rewakeSourceTimer,
		"hook_type":       "scheduler_job",
		"hook_name":       "scheduler:" + jobID,
		SchedulerKeyJobID: jobID,
		"name":            jobName,
	}
	return models.NewBusMessage(models.MessageTypeEvent, "scheduler", payload)
}

// publishRewake builds and publishes the timer rewake signal on the bus,
// mirroring the fire-and-forget style of the existing
// PublishExternalOnly("scheduler.job.completed") call beside which it is
// wired. Errors are logged, never returned: a rewake is advisory.
func (s *Scheduler) publishRewake(jobID, jobName string) {
	if s.bus == nil {
		return
	}
	msg, err := buildRewakeSignal(jobID, jobName)
	if err != nil {
		s.logger.Warn("scheduler: failed to build rewake signal",
			"job_id", jobID, "error", err)
		return
	}
	_ = s.bus.PublishExternalOnly(HookAsyncRewakeTopic, msg)
}
