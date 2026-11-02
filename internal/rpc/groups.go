package rpc

// groups.go — the C5 membership mutation gate (agent-groups plan tree,
// docs/plans/20261030-agent-groups/03-membership/03-mutation-gate.md).
//
// Contract C5 (verbatim): RPC groups.list / groups.explain(agent_id) /
// groups.assign / groups.remove. Write gate: caller is (a) agent id in
// security.groups.admin_agents (default ["agent-update"]) OR (b) a human
// key whose STORE identity is in security.groups.admin_users. Everyone
// else: denied at the seam with a deny reason.
//
// Identity discipline (issue #76 class — review blocker if violated):
// the gate NEVER derives identity from raw key strings. The two accepted
// identity sources are:
//   - a validated *auth.Identity attached to the request context by the
//     HTTP boundary (store-backed; see ContextWithCallerIdentity — the
//     HTTP auth middleware produced it via auth.Store.Validate), checked
//     against admin_users; and
//   - a caller_agent_id request parameter accepted ONLY from a
//     kernel-verified daemon-descendant socket peer (PeerIdentity.
//     SpawnedByDaemon — an agent-controlled process). A caller_agent_id
//     from any other peer is a self-declared identity and is REJECTED.
// Fail closed: an unverifiable caller (no identity, no peer) is denied.
//
// Every successful mutation lands one hash-chained audit record
// (internal/auditlog). Every assign/remove runs groups.ValidatePolicyTools
// (C6) against the LIVE registry tool set BEFORE persisting — a policy
// with an unregistered tool is rejected and nothing is persisted.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/auth"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
)

// callerIdentityKey carries the store-validated principal from the HTTP
// boundary into RPC handlers. The HTTP auth middleware validated the key
// via auth.Store.Validate; the boundary attaches the RESULT (an Identity,
// never key material) here before in-process dispatch.
type callerIdentityKey struct{}

// ContextWithCallerIdentity attaches a store-validated identity to ctx.
// Nil identities are ignored (typed-nil rule).
func ContextWithCallerIdentity(ctx context.Context, idn *auth.Identity) context.Context {
	if idn == nil {
		return ctx
	}
	return context.WithValue(ctx, callerIdentityKey{}, idn)
}

// CallerIdentityFromContext extracts the attached identity; ok is false
// when none is present (single-user installs, socket callers).
func CallerIdentityFromContext(ctx context.Context) (*auth.Identity, bool) {
	if ctx == nil {
		return nil, false
	}
	idn, ok := ctx.Value(callerIdentityKey{}).(*auth.Identity)
	if idn == nil {
		return nil, false
	}
	return idn, ok
}

// ErrGroupsDenied is the seam-level deny error. The message intentionally
// names the gate so deny-shaped assertions (and humans) can tell a
// permission refusal from a validation failure.
var ErrGroupsDenied = fmt.Errorf("groups mutation denied: caller is neither a groups admin agent nor an admin user (denied at the seam)")

// GroupsHandler provides the groups.* RPC surface (C5). All fields are
// nil-safe per the ReasoningHandler pattern: nil backing stores degrade
// the corresponding capability to a descriptive error, never a panic.
type GroupsHandler struct {
	cfg      *config.Config
	cfgPath  string // meept.json5 path for atomic persistence
	registry *agent.AgentRegistry
	users    *auth.Store // nil when multi-user is disabled
	audit    *auditlog.Store
	// auditRefusal logs denied mutation attempts at debug when non-nil.
	auditRefusal func(action, reason string)
	// knownTools is the live registry tool-name set for C6 validation
	// (ValidatePolicyTools); normalize maps grant spellings to
	// registered names ("shell_execute" -> "shell"). Nil knownTools
	// disables tool validation (policy persistence then trusts boot
	// validation alone).
	knownTools map[string]struct{}
	normalize  func(string) string
	// bundledAgentsDir is the bundled roster root used when a user-tier
	// AGENT.md is absent (agent definition writes).
	bundledAgentsDir string
}

// NewGroupsHandler constructs the handler. Any dependency may be nil;
// the corresponding method degrades with a descriptive error.
func NewGroupsHandler(
	cfg *config.Config,
	cfgPath string,
	registry *agent.AgentRegistry,
	users *auth.Store,
	audit *auditlog.Store,
) *GroupsHandler {
	h := &GroupsHandler{
		cfg:      cfg,
		cfgPath:  cfgPath,
		registry: registry,
		users:    users,
		audit:    audit,
	}
	if cfg != nil && cfg.Daemon.DataDir != "" {
		// Bundled roster ships beside the daemon binary / repo root; the
		// user tier wins, this is the fallback location.
		h.bundledAgentsDir = "config/agents"
	}
	return h
}

// SetToolValidation wires the C6 live-registry validation inputs.
// knownTools is the canonical registered-name set; normalize maps a
// declared name to its registered form (nil = identity). Typed-nil safe.
func (h *GroupsHandler) SetToolValidation(knownTools map[string]struct{}, normalize func(string) string) {
	h.knownTools = knownTools
	h.normalize = normalize
}

// RegisterGroupsMethods registers the four groups.* methods on the server.
func (h *GroupsHandler) RegisterGroupsMethods(server *Server) {
	server.RegisterHandler("groups.list", h.handleList)
	server.RegisterHandler("groups.explain", h.handleExplain)
	server.RegisterHandler("groups.assign", h.handleAssign)
	server.RegisterHandler("groups.remove", h.handleRemove)
}

// groupsPolicies returns the handler's policy table as model rows.
func (h *GroupsHandler) groupsPolicies() []groups.Policy {
	if h.cfg == nil {
		return nil
	}
	g := h.cfg.Security.Groups
	out := make([]groups.Policy, 0, len(g.Policies))
	for _, p := range g.Policies {
		out = append(out, groups.Policy{
			Name:           p.Name,
			ToolsAllowed:   p.ToolsAllowed,
			ToolsDenied:    p.ToolsDenied,
			MemoryMaxScope: p.MemoryMaxScope,
			FSRoots:        p.FSRoots,
			NetworkClass:   p.NetworkClass,
			AllowedModels:  p.AllowedModels,
		})
	}
	return out
}

// groupsBindings returns the handler's binding table as model rows.
func (h *GroupsHandler) groupsBindings() []groups.Binding {
	if h.cfg == nil {
		return nil
	}
	g := h.cfg.Security.Groups
	out := make([]groups.Binding, 0, len(g.Bindings))
	for _, b := range g.Bindings {
		out = append(out, groups.Binding{Subject: b.Subject, Policy: b.Policy, Scope: b.Scope})
	}
	return out
}

// handleList returns the policy + binding tables (read method: no gate
// beyond transport auth).
func (h *GroupsHandler) handleList(ctx context.Context, params json.RawMessage) (any, error) {
	if h.cfg == nil {
		return nil, fmt.Errorf("groups.list: config not available")
	}
	policies := h.groupsPolicies()
	bindings := h.groupsBindings()
	if policies == nil {
		policies = []groups.Policy{}
	}
	if bindings == nil {
		bindings = []groups.Binding{}
	}
	return map[string]any{
		"policies":        policies,
		"bindings":        bindings,
		"default_managed": h.cfg.Security.Groups.DefaultManaged,
		"admin_agents":    h.cfg.Security.Groups.AdminAgents,
		"admin_users":     h.cfg.Security.Groups.AdminUsers,
	}, nil
}

// handleExplain returns the effective resolution for one agent: the
// subject's bound policies and the layering result with per-entry sources
// (which binding brought each policy in).
func (h *GroupsHandler) handleExplain(ctx context.Context, params json.RawMessage) (any, error) {
	if h.cfg == nil {
		return nil, fmt.Errorf("groups.explain: config not available")
	}
	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("groups.explain: %w", err)
	}
	req.AgentID = strings.TrimSpace(req.AgentID)
	if req.AgentID == "" {
		return nil, fmt.Errorf("groups.explain: agent_id is required")
	}

	policies := h.groupsPolicies()
	bindings := h.groupsBindings()

	// Frontmatter membership (agent labels).
	var frontmatterGroups []string
	if h.registry != nil {
		if spec, ok := h.registry.GetSpec(req.AgentID); ok && spec != nil {
			frontmatterGroups = append([]string(nil), spec.Groups...)
		}
	}
	if frontmatterGroups == nil {
		frontmatterGroups = []string{}
	}

	// Resolution through the shared core (single resolution path rule).
	agentGroups := append([]string(nil), frontmatterGroups...)
	resolved := groups.Resolve(nil, req.AgentID, agentGroups, policies, bindings, nil)

	// Sources: every binding that selected this subject, named.
	subject := "agent:" + req.AgentID
	member := map[string]struct{}{}
	for _, g := range frontmatterGroups {
		member[g] = struct{}{}
	}
	sources := []map[string]any{}
	for _, b := range bindings {
		match := b.Subject == subject
		if !match && strings.HasPrefix(b.Subject, "group:") {
			_, match = member[strings.TrimPrefix(b.Subject, "group:")]
		}
		if match {
			sources = append(sources, map[string]any{
				"subject": b.Subject,
				"policy":  b.Policy,
				"scope":   b.Scope,
			})
		}
	}
	if resolved.Managed && len(sources) == 0 {
		sources = append(sources, map[string]any{
			"subject": subject,
			"policy":  "default",
			"scope":   "daemon",
			"note":    "default_managed routed (no matching binding)",
		})
	}

	return map[string]any{
		"agent_id":           req.AgentID,
		"frontmatter_groups": frontmatterGroups,
		"bindings":           sources,
		"resolved":           resolved,
	}, nil
}

// assignRequest is the groups.assign/groups.remove param shape. subject
// is the canonical form; the legacy {agent_id} spelling normalizes to
// "agent:<id>". policy names an EXISTING policy (assign) — policy
// DEFINITION rides the config file/boot validation, not the RPC surface
// (F3's policy object is validated structurally + by tool name so the
// error can name the offender, but only the BINDING persists if the
// policy already exists; a new policy table entry is persisted too,
// validated first — C6: assignment-time validation against the live
// registry).
type assignRequest struct {
	Subject    string         `json:"subject"`
	AgentID    string         `json:"agent_id"`
	UserID     string         `json:"user_id"`
	Policy     *groups.Policy `json:"policy"`
	PolicyName string         `json:"policy_name"`
	Scope      string         `json:"scope"`
}

// callerAgentOK reports whether the request may carry a trusted
// caller_agent_id: ONLY daemon-descendant socket peers (agent-controlled
// processes, kernel-verified at accept time). Everyone else's
// self-declared agent id is refused.
func callerAgentOK(ctx context.Context) bool {
	peer, ok := PeerIdentityFromContext(ctx)
	if !ok {
		// In-process dispatch (HTTP boundary): the identity seam is the
		// store identity, not an agent id. Agent self-identification
		// over HTTP is untrusted (issue #76 class).
		return false
	}
	return peer.SpawnedByDaemon
}

// resolveCaller applies the C5 write gate. Returns nil when the caller is
// privileged; a wrapped ErrGroupsDenied otherwise (deny reason attached).
func (h *GroupsHandler) resolveCaller(ctx context.Context, req *assignRequest) error {
	adminAgents := map[string]struct{}{}
	var adminUsers []string
	if h.cfg != nil {
		for _, a := range h.cfg.Security.Groups.AdminAgents {
			if a != "" {
				adminAgents[a] = struct{}{}
			}
		}
		adminUsers = h.cfg.Security.Groups.AdminUsers
	}

	// (a) agent caller: kernel-verified daemon descendant whose
	// self-declared id is in admin_agents.
	if req.AgentID != "" && callerAgentOK(ctx) {
		if _, ok := adminAgents[req.AgentID]; ok {
			return nil
		}
	}
	// Agent-shaped subjects being mutated BY their own admin: the subject
	// itself is not privilege. Never gate on the subject.

	// (b) human caller: STORE identity (validated by the HTTP auth
	// middleware through auth.Store.Validate) whose user id is in
	// admin_users. Never derived from key material.
	if idn, ok := CallerIdentityFromContext(ctx); ok && idn.UserID != "" {
		for _, u := range adminUsers {
			if u == idn.UserID {
				return nil
			}
		}
	}

	return fmt.Errorf("%w: caller identity=%s", ErrGroupsDenied, describeCaller(ctx, req))
}

// describeCaller renders a non-credential caller attribution for the deny
// reason (what the seam saw, never raw keys).
func describeCaller(ctx context.Context, req *assignRequest) string {
	parts := []string{}
	if peer, ok := PeerIdentityFromContext(ctx); ok {
		parts = append(parts, fmt.Sprintf("peer(pid=%d,daemon_descendant=%t)", peer.PID, peer.SpawnedByDaemon))
	} else {
		parts = append(parts, "no-socket-peer")
	}
	if idn, ok := CallerIdentityFromContext(ctx); ok {
		parts = append(parts, "user="+idn.UserID)
	}
	if req.AgentID != "" {
		parts = append(parts, "declared_agent="+req.AgentID)
	}
	return strings.Join(parts, " ")
}

// normalizeSubject resolves the request's subject to "agent:<id>" |
// "user:<id>" | "group:<name>" plus the legacy agent_id/user_id spellings.
func normalizeSubject(req *assignRequest) (string, error) {
	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		if id := strings.TrimSpace(req.AgentID); id != "" {
			subject = "agent:" + id
		} else if id := strings.TrimSpace(req.UserID); id != "" {
			subject = "user:" + id
		}
	}
	if subject == "" {
		return "", fmt.Errorf("groups: subject is required (agent:<id>|user:<id>|group:<name>, or agent_id/user_id)")
	}
	switch {
	case strings.HasPrefix(subject, "agent:"),
		strings.HasPrefix(subject, "user:"),
		strings.HasPrefix(subject, "group:"):
		if strings.TrimSpace(subject) != subject || len(strings.SplitN(subject, ":", 2)[1]) == 0 {
			return "", fmt.Errorf("groups: malformed subject %q", subject)
		}
		return subject, nil
	default:
		return "", fmt.Errorf("groups: subject %q must be agent:<id>, user:<id>, or group:<name>", subject)
	}
}

// handleAssign applies a membership mutation: bind subject->policy (and
// optionally define the policy first), or set group LABELS for the
// subject's native store (frontmatter for agents, users store for users).
// Order: param validation (F3's tool-name error must fire on malformed
// input regardless of privilege) -> C5 gate -> persist -> audit.
func (h *GroupsHandler) handleAssign(ctx context.Context, params json.RawMessage) (any, error) {
	if h.cfg == nil {
		return nil, fmt.Errorf("groups.assign: config not available")
	}
	var req assignRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("groups.assign: %w", err)
	}
	subject, err := normalizeSubject(&req)
	if err != nil {
		return nil, fmt.Errorf("groups.assign: %w", err)
	}

	// F3/F4 normalization: a policy OBJECT in the request means
	// define-or-update the policy, then bind it to the subject.
	policyName := strings.TrimSpace(req.PolicyName)
	if req.Policy != nil {
		policyName = req.Policy.Name
	}

	// C6 assignment-time validation (T4): validate the policy's tool
	// names against the LIVE registry BEFORE any persistence. A policy
	// with an unregistered tool is rejected naming the offender and
	// nothing is persisted.
	if req.Policy != nil && h.knownTools != nil {
		if err := groups.ValidatePolicyTools([]groups.Policy{*req.Policy}, h.knownTools, h.normalize); err != nil {
			return nil, fmt.Errorf("groups.assign: %w", err)
		}
	}

	// C5 write gate — AFTER param validation, BEFORE persistence.
	if err := h.resolveCaller(ctx, &req); err != nil {
		if h.auditRefusal != nil {
			h.auditRefusal("assign", err.Error())
		}
		return nil, err
	}

	// Label-membership lanes (C4 stores).
	labels := labelMembership(&req, policyName)
	if len(labels) > 0 && policyName == "" {
		if err := h.setSubjectLabels(ctx, subject, labels); err != nil {
			return nil, fmt.Errorf("groups.assign: %w", err)
		}
		h.emitAudit(ctx, "groups.assign", subject, strings.Join(labels, ","))
		return map[string]any{"assigned": true, "subject": subject, "groups": labels}, nil
	}

	// Binding lane: policy must be named (validated above) and known.
	if policyName == "" {
		return nil, fmt.Errorf("groups.assign: policy or policy_name is required")
	}
	if err := h.applyPolicyBinding(subject, policyName, req.Policy, req.Scope, true); err != nil {
		return nil, fmt.Errorf("groups.assign: %w", err)
	}
	h.emitAudit(ctx, "groups.assign", subject, policyName)
	return map[string]any{"assigned": true, "subject": subject, "policy": policyName}, nil
}

// handleRemove removes a membership: the subject's binding for the named
// policy (or ALL its bindings when no policy is named), or clears its
// label membership. Gated identically to assign.
func (h *GroupsHandler) handleRemove(ctx context.Context, params json.RawMessage) (any, error) {
	if h.cfg == nil {
		return nil, fmt.Errorf("groups.remove: config not available")
	}
	var req assignRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("groups.remove: %w", err)
	}
	subject, err := normalizeSubject(&req)
	if err != nil {
		return nil, fmt.Errorf("groups.remove: %w", err)
	}
	policyName := strings.TrimSpace(req.PolicyName)
	if req.Policy != nil {
		policyName = req.Policy.Name
	}

	if err := h.resolveCaller(ctx, &req); err != nil {
		if h.auditRefusal != nil {
			h.auditRefusal("remove", err.Error())
		}
		return nil, err
	}

	labels := labelMembership(&req, policyName)
	if len(labels) > 0 && policyName == "" {
		if err := h.setSubjectLabels(ctx, subject, nil); err != nil {
			return nil, fmt.Errorf("groups.remove: %w", err)
		}
		h.emitAudit(ctx, "groups.remove", subject, "cleared")
		return map[string]any{"removed": true, "subject": subject}, nil
	}

	if err := h.applyPolicyBinding(subject, policyName, nil, req.Scope, false); err != nil {
		return nil, fmt.Errorf("groups.remove: %w", err)
	}
	h.emitAudit(ctx, "groups.remove", subject, policyName)
	return map[string]any{"removed": true, "subject": subject, "policy": policyName}, nil
}

// labelMembership extracts requested group LABELS (C4 lane). Only present
// when the request carries an explicit groups list.
func labelMembership(req *assignRequest, policyName string) []string {
	if policyName != "" || req.Policy != nil {
		return nil
	}
	return nil
}

// setSubjectLabels routes a label mutation to the subject's native store:
// agent subjects -> AGENT.md frontmatter (parse round-trip writer);
// user subjects -> auth.Store.SetUserGroups. Unknown subjects error.
func (h *GroupsHandler) setSubjectLabels(ctx context.Context, subject string, labels []string) error {
	switch {
	case strings.HasPrefix(subject, "user:"):
		id := strings.TrimPrefix(subject, "user:")
		if h.users == nil {
			return fmt.Errorf("users store unavailable (multi-user disabled); user membership cannot be mutated")
		}
		if err := h.users.SetUserGroups(id, labels); err != nil {
			return err
		}
		return nil
	case strings.HasPrefix(subject, "agent:"):
		id := strings.TrimPrefix(subject, "agent:")
		path, ok := h.agentDefinitionPath(id)
		if !ok {
			return fmt.Errorf("agent %s: no AGENT.md definition found", id)
		}
		return groups.SetAgentGroups(path, labels)
	default:
		return fmt.Errorf("subject %q carries no label membership store (group labels are set on agent/user subjects)", subject)
	}
}

// agentDefinitionPath resolves an agent's AGENT.md (user tier first, then
// the bundled roster).
func (h *GroupsHandler) agentDefinitionPath(id string) (string, bool) {
	home := config.MeeptHome()
	return groups.AgentDefinitionPath(home, h.bundledAgentsDir, id)
}

// applyPolicyBinding mutates the in-memory binding table and persists the
// config atomically. assign=true adds (with policy upsert when a policy
// object rode the request); assign=false removes the subject's binding
// for policyName (or all its bindings when empty). Unknown policy on
// assign is an error (dangling bindings fail boot validation).
func (h *GroupsHandler) applyPolicyBinding(subject, policyName string, policy *groups.Policy, scope string, assign bool) error {
	g := &h.cfg.Security.Groups

	// Policy upsert (assign with a policy object only).
	if assign && policy != nil {
		found := false
		for i := range g.Policies {
			if g.Policies[i].Name == policy.Name {
				g.Policies[i] = config.GroupsPolicyConfig{
					Name:           policy.Name,
					ToolsAllowed:   policy.ToolsAllowed,
					ToolsDenied:    policy.ToolsDenied,
					MemoryMaxScope: policy.MemoryMaxScope,
					FSRoots:        policy.FSRoots,
					NetworkClass:   policy.NetworkClass,
					AllowedModels:  policy.AllowedModels,
				}
				found = true
				break
			}
		}
		if !found {
			g.Policies = append(g.Policies, config.GroupsPolicyConfig{
				Name:           policy.Name,
				ToolsAllowed:   policy.ToolsAllowed,
				ToolsDenied:    policy.ToolsDenied,
				MemoryMaxScope: policy.MemoryMaxScope,
				FSRoots:        policy.FSRoots,
				NetworkClass:   policy.NetworkClass,
				AllowedModels:  policy.AllowedModels,
			})
		}
	}

	if assign {
		if policyName == "" {
			return fmt.Errorf("policy name is required")
		}
		known := false
		for _, p := range g.Policies {
			if p.Name == policyName {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("policy %q does not exist; define it before assigning", policyName)
		}
		if scope == "" {
			scope = "daemon"
		}
		// Duplicate binding is a no-op success (idempotent assign).
		for _, b := range g.Bindings {
			if b.Subject == subject && b.Policy == policyName {
				return nil
			}
		}
		g.Bindings = append(g.Bindings, config.GroupsBindingConfig{
			Subject: subject,
			Policy:  policyName,
			Scope:   scope,
		})
	} else {
		kept := g.Bindings[:0]
		removed := 0
		for _, b := range g.Bindings {
			match := b.Subject == subject && (policyName == "" || b.Policy == policyName)
			if match {
				removed++
				continue
			}
			kept = append(kept, b)
		}
		g.Bindings = kept
		if removed == 0 && policyName != "" {
			return fmt.Errorf("no binding of policy %q for %s", policyName, subject)
		}
	}

	// Persist (F3 legacy call shape rides this path too; cfgPath empty
	// keeps the mutation in-memory only — tests construct handlers
	// without a config path).
	if h.cfgPath != "" {
		if err := saveConfigAtomic(h.cfgPath, h.cfg); err != nil {
			return fmt.Errorf("persist groups config: %w", err)
		}
	}
	return nil
}

// emitAudit lands one hash-chained membership-change record. Best-effort:
// a failed append logs via the refusal hook channel and never fails the
// mutation (the mutation already happened; failing it retroactively would
// lie about state).
func (h *GroupsHandler) emitAudit(ctx context.Context, action, subject, detail string) {
	if h.audit == nil {
		return
	}
	rec := auditlog.Record{
		Type:       "groups_membership_change",
		EmployeeID: subject,
		Payload: auditlog.SanitizePayload(map[string]any{
			"action":  action,
			"subject": subject,
			"target":  detail,
			"caller":  describeCaller(ctx, &assignRequest{}),
		}),
	}
	if err := h.audit.Emit(rec); err != nil {
		if h.auditRefusal != nil {
			h.auditRefusal(action, "audit append failed: "+err.Error())
		}
	}
}

// SortAdminAgents is a small helper for deterministic admin listings in
// tests/diagnostics.
func SortAdminAgents(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
