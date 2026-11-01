// slack_interactions.go wires the Slack approval-channel interaction
// endpoint (POST /api/v1/slack/interactions) into the unified HTTP server
// (hitl-notifications leaf 01, Contract C3).
//
// The handler lives in internal/comm/slack; this file is only the
// registration seam, mirroring the WithAgentHandlers/RouteRegistrar
// pattern. The route is registered ONLY when the daemon wires it (slack
// enabled in config) - default-off means the route does not exist at all.
//
// Auth note: Slack cannot send a bearer API key, so the route is exempt
// from the bearer-auth middleware (same exemption family as the pairing
// handshake). Its authentication is the verified v0 request signature plus
// the allowed_user_ids check INSIDE the handler; both are enforced there,
// never by the presence of the route.
package http

import "net/http"

// WithSlackInteractions registers the Slack interaction handler. h may be
// nil (no-op): a disabled channel must not create the route.
func WithSlackInteractions(h http.Handler) ServerOption {
	return func(s *Server) {
		if h != nil {
			s.slackInteractions = h
		}
	}
}

// RegisterSlackInteractions is the explicit registration helper used by
// setupRESTRoutes; exposed for tests that build a bare mux.
func (s *Server) RegisterSlackInteractions(mux *http.ServeMux) {
	if s.slackInteractions != nil {
		mux.HandleFunc("POST /api/v1/slack/interactions", s.slackInteractions.ServeHTTP)
	}
}
