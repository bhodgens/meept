package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

const defaultMaxRouteDepth = 5

// RouteParams holds the parameters for a routing decision.
type RouteParams struct {
	Report  *AgentReport
	Action  RouteAction
	AgentID string
	Depth   int
}

// RouteResult holds the result of routing a completed agent's work.
type RouteResult struct {
	FinalResponse string
	ForceNotify   bool
	Depth         int
}

// ReportRouter executes routing decisions after an agent completes.
// It replaces the dead-end DetermineRouteAction call in the dispatcher.
type ReportRouter struct {
	registry   *AgentRegistry
	dispatcher *Dispatcher
	bus        interface{ Publish(string, any) }
	logger     *slog.Logger
	maxDepth   int
}

// ReportRouterConfig configures the report router.
type ReportRouterConfig struct {
	Registry   *AgentRegistry
	Dispatcher *Dispatcher
	Bus        interface{ Publish(string, any) }
	Logger     *slog.Logger
	MaxDepth   int
}

// NewReportRouter creates a new report router.
func NewReportRouter(cfg ReportRouterConfig) *ReportRouter {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultMaxRouteDepth
	}
	return &ReportRouter{
		registry:   cfg.Registry,
		dispatcher: cfg.Dispatcher,
		bus:        cfg.Bus,
		logger:     cfg.Logger.With("component", "report-router"),
		maxDepth:   maxDepth,
	}
}

// Route determines what to do after an agent completes its work.
// For RouteActionClose, it returns the display response.
// For RouteActionRoute, it would run the next agent (handled by the dispatcher loop).
// For RouteActionNotifyUser/RouteActionNotifyError, it flags that user input is needed.
// At max depth, all actions become RouteActionNotifyUser.
func (r *ReportRouter) Route(ctx context.Context, params RouteParams) RouteResult {
	r.logger.Debug("routing",
		"action", params.Action.String(),
		"agent", params.AgentID,
		"depth", params.Depth,
	)

	// Force notify at max depth to prevent infinite handoff loops
	if params.Depth >= r.maxDepth {
		r.logger.Warn("max route depth reached, forcing user notification",
			"depth", params.Depth,
			"max", r.maxDepth,
		)
		return RouteResult{
			FinalResponse: r.formatAccumulatedResponse(params),
			ForceNotify:   true,
			Depth:         params.Depth,
		}
	}

	switch params.Action {
	case RouteActionClose:
		return RouteResult{
			FinalResponse: r.formatCloseResponse(params),
			Depth:         params.Depth,
		}

	case RouteActionRoute:
		// The actual agent handoff is done by the dispatcher's RouteToAgent loop.
		// Here we just indicate that routing should continue.
		return RouteResult{
			FinalResponse: "",
			Depth:         params.Depth + 1,
		}

	case RouteActionNotifyUser:
		return RouteResult{
			FinalResponse: r.formatNotifyUserResponse(params),
			ForceNotify:   true,
			Depth:         params.Depth,
		}

	case RouteActionNotifyError:
		return RouteResult{
			FinalResponse: r.formatErrorResponse(params),
			ForceNotify:   true,
			Depth:         params.Depth,
		}

	default:
		return RouteResult{
			FinalResponse: r.formatCloseResponse(params),
			Depth:         params.Depth,
		}
	}
}

func (r *ReportRouter) formatCloseResponse(params RouteParams) string {
	if params.Report == nil {
		return ""
	}
	var parts []string
	if len(params.Report.Accomplished) > 0 {
		parts = append(parts, params.Report.Accomplished...)
	}
	if len(params.Report.Observations) > 0 {
		parts = append(parts, params.Report.Observations...)
	}
	return strings.Join(parts, "; ")
}

func (r *ReportRouter) formatAccumulatedResponse(params RouteParams) string {
	if params.Report == nil {
		return "task reached maximum routing depth"
	}
	return fmt.Sprintf("routing depth limit reached after %d handoffs. accomplished: %s",
		params.Depth,
		strings.Join(params.Report.Accomplished, ", "),
	)
}

func (r *ReportRouter) formatNotifyUserResponse(params RouteParams) string {
	if params.Report == nil {
		return "user input needed"
	}
	var parts []string
	if params.Report.DecisionContext != "" {
		parts = append(parts, params.Report.DecisionContext)
	}
	if len(params.Report.NotDone) > 0 {
		parts = append(parts, "remaining: "+strings.Join(params.Report.NotDone, ", "))
	}
	return strings.Join(parts, "; ")
}

func (r *ReportRouter) formatErrorResponse(params RouteParams) string {
	if params.Report == nil {
		return "agent failed"
	}
	if len(params.Report.Issues) > 0 {
		return "error: " + strings.Join(params.Report.Issues, "; ")
	}
	return "agent failed with unspecified error"
}
