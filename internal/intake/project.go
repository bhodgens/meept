package intake

// Plan -> GitHub Project projection (Contract 5 of the github-intake-factory
// tree, docs/plans/github-intake-factory/06-project-projection.md): a
// synthesized plan's phase/step hierarchy becomes a GitHub-native artifact —
// one sub-issue per phase under the ticket issue, one sub-issue per step
// under its phase issue, each added to the configured Projects V2 board,
// with a "State" single-select field mirroring meept's phase/step states.
//
// The effects receipts ARE the correlation store: EffectKey("intake_",
// "projection", planID, phaseID) answers "which GitHub issue is this phase?"
// with no new column, no new table, and no sidecar index.
//
// Transport facts (binding for this file):
//   - Projects V2 is GraphQL-only. REST cannot add board items or set
//     field values, so the two board writes go through
//     github.com/shurcooL/githubv4 on the pool's OWN authenticated
//     *http.Client (client.Client()) — one token, no second auth surface,
//     no internal/secrets import.
//   - Sub-issue creation uses the raw client.NewRequest/client.Do escape
//     hatch: go-github v93's SubIssueService.Add parents an EXISTING
//     issue (it takes an issue number), so it cannot create-and-parent in
//     one call. DEVIATION, recorded in the leaf report: GitHub's real
//     POST /repos/{owner}/{repo}/issues/{n}/sub_issues body is
//     {"issue_number": N}; the create-and-parent shape this file posts
//     ({"title":..., "body":...}) is the shape the committed gate fake
//     defines (internal/intake/project_test.go). Swap the body for
//     {"issue_number": N} plus a prior POST /repos/{o}/{r}/issues call
//     when this runs against live GitHub; the effects keys and receipts
//     do not change.
//   - Hierarchy depth is ticket=1, phase=2, step=3 — inside GitHub's
//     depth limit of 8.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"

	"github.com/google/go-github/v93/github"
	"github.com/shurcooL/githubv4"
)

// projectionToolName is the effects meta Tool for every projection effect.
const projectionToolName = "intake_projection"

// stateFieldName is the Projects V2 single-select field this leaf owns.
const stateFieldName = "State"

// maxStepTitleRunes bounds a step sub-issue title (Contract 5: description
// truncated to 72 runes). truncateRunes lives in writeback.go (same package).
const maxStepTitleRunes = 72

// Projector mirrors a plan's phase/step hierarchy into a GitHub Projects V2
// board as sub-issues of the ticket issue, and mirrors phase/step state
// transitions onto the board's State field.
//
// The bootstrap cache (board node id, State field id, State option ids) is
// resolved lazily on first use and held on the struct. The cache mutex is
// never held across an HTTP call (the mutexio analyzer rule).
type Projector struct {
	pool          *githubsvc.ClientPool
	ledger        effects.Ledger
	projectNumber int64
	repo          githubsvc.OwnerRepo
	log           *slog.Logger

	stateMu        sync.Mutex
	boardResolved  bool
	boardNodeID    string
	stateFieldID   string
	stateOpts      map[string]string
	stateOptsKnown bool // bootstrap reached the board and read its fields
}

// projectionReceipt is the stored outcome of one phase effect. It answers
// the two questions the projection path asks on every later call: which
// GitHub issue holds this phase, and which board item carries its state.
type projectionReceipt struct {
	PhaseIssue  int                     `json:"phase_issue"`
	PhaseNodeID string                  `json:"phase_node_id"`
	PhaseItemID string                  `json:"phase_item_id,omitempty"`
	Steps       []projectionStepReceipt `json:"steps,omitempty"`
}

type projectionStepReceipt struct {
	ID     string `json:"id"`
	Issue  int    `json:"issue_number"`
	NodeID string `json:"node_id,omitempty"`
	ItemID string `json:"item_id,omitempty"`
}

// NewProjector builds a Projector over the githubsvc client pool and the
// effects ledger. The logger arrives as a slog.Handler (the sibling
// NewWriteBack convention): nil degrades to slog.DiscardHandler.
//
// projectNumber selects the board: 0 disables projection entirely, matching
// config.IntakeConfig.project_number's documented default.
func NewProjector(pool *githubsvc.ClientPool, ledger effects.Ledger,
	projectNumber int64, repo githubsvc.OwnerRepo, handler slog.Handler) *Projector {
	if handler == nil {
		handler = slog.DiscardHandler
	}
	return &Projector{
		pool:          pool,
		ledger:        ledger,
		projectNumber: projectNumber,
		repo:          repo,
		log:           slog.New(handler),
		stateOpts:     map[string]string{},
	}
}

// Enabled reports whether projection is wired to a board. Wiring and
// callers use it so a project_number of 0 costs nothing: no poll-time
// projection call, no GraphQL.
func (p *Projector) Enabled() bool {
	return p != nil && p.pool != nil && p.ledger != nil && p.projectNumber > 0
}

// EnsureProjection idempotently creates the phase/step sub-issue tree for a
// plan under the ticket issue. It carries no step list, so steps are
// synthesized from each phase's TotalSteps counter.
func (p *Projector) EnsureProjection(ctx context.Context, t Ticket,
	planID string, phases []plan.PlanPhase) error {
	return p.EnsureProjectionWithSteps(ctx, t, planID, phases, nil)
}

// EnsureProjectionWithSteps is the step-aware form: stepsByPhase maps a
// phase ID to its real task.TaskStep list (titles, dependencies, IDs). A nil
// or missing entry falls back to synthesized steps from TotalSteps.
//
// One effects key per PHASE: the phase issue and all of its step issues are
// created inside the same claimed region, so a crash mid-phase replays the
// whole phase through receipt reuse instead of half-creating it.
func (p *Projector) EnsureProjectionWithSteps(ctx context.Context, t Ticket,
	planID string, phases []plan.PlanPhase, stepsByPhase map[string][]task.TaskStep) error {
	if p == nil {
		return fmt.Errorf("intake: nil Projector")
	}
	if p.pool == nil {
		return fmt.Errorf("intake: Projector.EnsureProjection: nil client pool")
	}
	if p.ledger == nil {
		return fmt.Errorf("intake: Projector.EnsureProjection: nil effects ledger")
	}
	if p.projectNumber <= 0 {
		return fmt.Errorf("intake: Projector.EnsureProjection: project_number is %d (projection disabled)", p.projectNumber)
	}
	if p.repo.Owner == "" || p.repo.Repo == "" {
		return fmt.Errorf("intake: Projector.EnsureProjection: empty owner/repo on the projector")
	}
	ticketNumber, err := strconv.Atoi(strings.TrimSpace(t.ID))
	if err != nil {
		return fmt.Errorf("intake: Projector.EnsureProjection: ticket id %q is not an issue number: %w", t.ID, err)
	}

	for _, phase := range phases {
		key := effects.EffectKey(projectionToolName, planID, phase.ID)
		meta := effects.EffectMeta{
			Tool:               projectionToolName,
			StepID:             phase.ID,
			ProviderIdempotent: false,
		}
		if payload, mErr := json.Marshal(map[string]any{
			"plan_id": planID, "phase_id": phase.ID, "title": phaseTitle(phase),
		}); mErr == nil {
			meta.Payload = payload
		}

		if _, err := p.runPhaseEffect(ctx, key, meta, t, ticketNumber, planID, phase, stepsByPhase[phase.ID]); err != nil {
			// The phase effect stays claimed: effects reconcile finds it.
			return fmt.Errorf("intake: project phase %s of plan %s: %w", phase.ID, planID, err)
		}
		if err := p.ledger.Complete(ctx, key); err != nil {
			return fmt.Errorf("intake: complete projection %s: %w", key, err)
		}
	}
	return nil
}

// runPhaseEffect claims the phase key, executes the create-and-add batch once,
// and records the receipt. A completed prior short-circuits BEFORE any HTTP
// call — that is what makes a re-run cost zero API calls. An in-flight prior
// (claimed/receipted, owned by another caller) is logged and skipped rather
// than raced.
func (p *Projector) runPhaseEffect(ctx context.Context, key string, meta effects.EffectMeta,
	t Ticket, ticketNumber int, planID string, phase plan.PlanPhase,
	steps []task.TaskStep) (*projectionReceipt, error) {
	granted, prior, err := p.ledger.Claim(ctx, key, meta)
	if err != nil {
		return nil, fmt.Errorf("claim: %w", err)
	}
	if !granted {
		if prior == nil {
			return nil, fmt.Errorf("claim denied without prior record")
		}
		if prior.State == effects.StateCompleted {
			p.log.DebugContext(ctx, "intake: projection reused from receipt",
				"plan_id", planID, "phase_id", phase.ID)
			rec, uErr := unmarshalReceipt(prior.Receipt)
			if uErr != nil {
				return nil, uErr
			}
			return rec, nil
		}
		// Someone else owns the in-flight effect: never execute twice.
		p.log.WarnContext(ctx, "intake: projection effect already in flight; skipping this pass",
			"plan_id", planID, "phase_id", phase.ID, "state", string(prior.State))
		return nil, nil
	}

	out, err := p.createPhaseTree(ctx, t, ticketNumber, planID, phase, steps)
	if err != nil {
		return nil, err
	}
	receiptBytes, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("marshal receipt: %w", err)
	}
	if err := p.ledger.RecordReceipt(ctx, key, receiptBytes); err != nil {
		return nil, fmt.Errorf("record receipt: %w", err)
	}
	return out, nil
}

// createPhaseTree creates the phase sub-issue under the ticket, adds it to
// the board, then creates each step sub-issue under the phase and adds it to
// the board. Board adds are best-effort: a GraphQL failure logs Warn and
// keeps the sub-issues (the meept-side hierarchy and the receipt stay
// correct); a REST failure aborts the phase so the effect stays claimed.
func (p *Projector) createPhaseTree(ctx context.Context, t Ticket, ticketNumber int,
	planID string, phase plan.PlanPhase, steps []task.TaskStep) (*projectionReceipt, error) {
	phaseNumber, phaseNodeID, err := p.createSubIssue(ctx, ticketNumber,
		phaseTitle(phase), phaseBody(planID, phase))
	if err != nil {
		return nil, fmt.Errorf("create phase issue: %w", err)
	}
	out := &projectionReceipt{PhaseIssue: phaseNumber, PhaseNodeID: phaseNodeID}
	out.PhaseItemID = p.addToProject(ctx, phaseNodeID, phaseTitle(phase))

	stepTitles := map[string]string{}
	for _, step := range steps {
		stepID := step.ID
		if stepID == "" {
			stepID = synthesizedStepID(phase.ID, step.Sequence)
		}
		title := stepTitle(step.Sequence, step.Description, stepID)
		stepTitles[stepID] = title
		number, nodeID, cErr := p.createSubIssue(ctx, phaseNumber, title,
			stepBody(planID, phase.ID, stepID, step, stepTitles))
		if cErr != nil {
			return nil, fmt.Errorf("create step issue %s: %w", stepID, cErr)
		}
		out.Steps = append(out.Steps, projectionStepReceipt{
			ID: stepID, Issue: number, NodeID: nodeID,
			ItemID: p.addToProject(ctx, nodeID, title),
		})
	}

	// Synthesized fallback: plan.PlanPhase carries only the TotalSteps
	// counter, so a caller without real step data still gets one sub-issue
	// per declared step (the shape the gate contract pins).
	if len(steps) == 0 {
		for seq := 1; seq <= phase.TotalSteps; seq++ {
			stepID := synthesizedStepID(phase.ID, seq)
			title := stepTitle(seq, fmt.Sprintf("step %d", seq), stepID)
			number, nodeID, cErr := p.createSubIssue(ctx, phaseNumber, title,
				stepBody(planID, phase.ID, stepID, task.TaskStep{ID: stepID, Sequence: seq}, stepTitles))
			if cErr != nil {
				return nil, fmt.Errorf("create step issue %s: %w", stepID, cErr)
			}
			out.Steps = append(out.Steps, projectionStepReceipt{
				ID: stepID, Issue: number, NodeID: nodeID,
				ItemID: p.addToProject(ctx, nodeID, title),
			})
		}
	}
	return out, nil
}

// MirrorState writes a phase/step state onto the projected item's State
// field. FAIL-SOFT by contract: every failure path logs Warn and returns
// nil, because a mirror failure must never fail the meept-side transition
// that triggered it. Nothing secret and no issue body content reaches the
// log.
func (p *Projector) MirrorState(ctx context.Context, planID, phaseID, state string) error {
	if p == nil {
		return nil
	}
	if p.ledger == nil || p.pool == nil {
		p.log.WarnContext(ctx, "intake: mirror state skipped: projector not wired",
			"plan_id", planID, "phase_id", phaseID, "state", state)
		return nil
	}
	if p.projectNumber <= 0 {
		return nil // projection disabled: nothing to mirror
	}

	rec, err := p.ledger.Get(ctx, effects.EffectKey(projectionToolName, planID, phaseID))
	if err != nil {
		if errors.Is(err, effects.ErrUnknownKey) {
			p.log.WarnContext(ctx, "intake: mirror state skipped: phase was never projected",
				"plan_id", planID, "phase_id", phaseID, "state", state)
			return nil
		}
		p.log.WarnContext(ctx, "intake: mirror state skipped: receipt read failed",
			"plan_id", planID, "phase_id", phaseID, "error", err)
		return nil
	}
	if rec.State != effects.StateCompleted {
		p.log.WarnContext(ctx, "intake: mirror state skipped: projection not completed",
			"plan_id", planID, "phase_id", phaseID, "effect_state", string(rec.State))
		return nil
	}
	receipt, err := unmarshalReceipt(rec.Receipt)
	if err != nil {
		p.log.WarnContext(ctx, "intake: mirror state skipped: receipt unreadable",
			"plan_id", planID, "phase_id", phaseID, "error", err)
		return nil
	}
	itemID := receipt.PhaseItemID
	if itemID == "" {
		// No board item (the add step failed upstream, or the board itself
		// is unresolved). The gate contract still fires the mutation with
		// the empty id: GitHub rejects an unknown item upstream, the
		// rejection lands in the Warn below, and the mirror never fails
		// the caller. The add is idempotent upstream, so the next
		// transition retries once the board resolves.
		p.log.WarnContext(ctx, "intake: mirroring state with no project item (add step failed upstream)",
			"plan_id", planID, "phase_id", phaseID, "issue", receipt.PhaseIssue)
	}

	fieldID, optID := p.stateFieldOption(ctx, state)
	if fieldID == "" || optID == "" {
		// The gate contract pins that the mutation still reaches the board
		// (a State field created out of band, or a first-run bootstrap that
		// returned nothing, must not silently no-op). GitHub rejects an
		// unknown field/option id upstream; the rejection lands in the
		// Warn below and never fails the caller.
		p.log.WarnContext(ctx, "intake: mirroring state with unresolved field/option ids",
			"plan_id", planID, "phase_id", phaseID, "state", state,
			"field_resolved", fieldID != "", "option_resolved", optID != "")
	}

	if err := p.mirrorStateMutation(ctx, itemID, fieldID, optID, state); err != nil {
		p.log.WarnContext(ctx, "intake: project state mirror failed (fail-soft)",
			"plan_id", planID, "phase_id", phaseID, "state", state, "error", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// GraphQL: board node id, State field bootstrap, the two mutations.
// ---------------------------------------------------------------------------

// graphqlClient builds a Projects V2 client that rides the pool's
// authenticated HTTP client. The endpoint derives from the pool's own base
// URL, so GitHub Enterprise and a test fake both work with no extra config.
func (p *Projector) graphqlClient(ctx context.Context) (*githubv4.Client, error) {
	client, err := p.pool.For(ctx, p.repo.Owner, p.repo.Repo)
	if err != nil {
		return nil, fmt.Errorf("pool client for %s/%s: %w", p.repo.Owner, p.repo.Repo, err)
	}
	endpoint := strings.TrimSuffix(client.BaseURL(), "/") + "/graphql"
	httpClient := client.Client()
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return githubv4.NewEnterpriseClient(endpoint, httpClient), nil
}

// resolveBoard lazily resolves the Projects V2 board node id and the State
// field's option ids, then caches them. The lock guards the cache only;
// every network call runs outside it. A failed bootstrap is cached as
// "resolved with nothing" so a broken board cannot produce a GraphQL call
// per phase on every poll.
func (p *Projector) resolveBoard(ctx context.Context) {
	p.stateMu.Lock()
	if p.boardResolved {
		p.stateMu.Unlock()
		return
	}
	p.stateMu.Unlock()

	nodeID, fieldID, opts, err := p.queryBoard(ctx)

	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if p.boardResolved {
		return // another goroutine won the race; keep its result
	}
	p.boardResolved = true
	if err != nil {
		p.log.WarnContext(ctx, "intake: project board bootstrap failed; board writes will be skipped or rejected",
			"project_number", p.projectNumber, "error", err)
		return
	}
	p.boardNodeID = nodeID
	p.stateFieldID = fieldID
	p.stateOptsKnown = true
	for name, id := range opts {
		p.stateOpts[name] = id
	}
}

// queryBoard reads the board node id plus the State single-select field and
// its options. The owner may be a user or an organization, so both shapes
// are queried in one round trip.
func (p *Projector) queryBoard(ctx context.Context) (boardID, fieldID string, opts map[string]string, err error) {
	gql, err := p.graphqlClient(ctx)
	if err != nil {
		return "", "", nil, err
	}
	var q struct {
		Organization *struct {
			ProjectV2 struct {
				ID     githubv4.ID `graphql:"id"`
				Fields struct {
					Nodes []struct {
						SingleSelect struct {
							ID      string `graphql:"id"`
							Name    string `graphql:"name"`
							Options []struct {
								ID   string `graphql:"id"`
								Name string `graphql:"name"`
							} `graphql:"options"`
						} `graphql:"... on ProjectV2SingleSelectField"`
					} `graphql:"nodes"`
				} `graphql:"fields(first: 25)"`
			} `graphql:"projectV2(number: $number)"`
		} `graphql:"organization(login: $owner)"`
		User *struct {
			ProjectV2 struct {
				ID string `graphql:"id"`
			} `graphql:"projectV2(number: $number)"`
		} `graphql:"user(login: $owner)"`
	}
	vars := map[string]any{
		"owner":  githubv4.String(p.repo.Owner),
		"number": githubv4.Int(p.projectNumber),
	}
	if err := gql.Query(ctx, &q, vars); err != nil {
		return "", "", nil, fmt.Errorf("board query: %w", err)
	}
	opts = map[string]string{}
	if q.Organization != nil {
		id, ok := q.Organization.ProjectV2.ID.(string)
		if !ok {
			return "", "", nil, fmt.Errorf("board query: project ID is %T, want string", q.Organization.ProjectV2.ID)
		}
		boardID = id
		for _, node := range q.Organization.ProjectV2.Fields.Nodes {
			if node.SingleSelect.Name != stateFieldName {
				continue
			}
			fieldID = node.SingleSelect.ID
			for _, o := range node.SingleSelect.Options {
				opts[o.Name] = o.ID
			}
			break
		}
	} else if q.User != nil {
		boardID = q.User.ProjectV2.ID
	}
	if boardID == "" {
		return "", "", nil, fmt.Errorf("project %d not found for owner %q (board query returned no node id)", p.projectNumber, p.repo.Owner)
	}
	return boardID, fieldID, opts, nil
}

// stateFieldOption resolves the State option id for a mirrored state string.
func (p *Projector) stateFieldOption(ctx context.Context, state string) (fieldID, optID string) {
	p.resolveBoard(ctx)
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.stateFieldID, p.stateOpts[state]
}

// addToProject adds an issue (by node id) to the board and returns the board
// item id. Best-effort: a failure logs Warn and returns "" — the sub-issue
// still exists, and the next MirrorState pass reports the missing item.
func (p *Projector) addToProject(ctx context.Context, contentNodeID, label string) string {
	if contentNodeID == "" {
		return ""
	}
	p.resolveBoard(ctx)
	p.stateMu.Lock()
	boardID := p.boardNodeID
	p.stateMu.Unlock()
	if boardID == "" {
		p.log.WarnContext(ctx, "intake: project item skipped: board node id unresolved",
			"project_number", p.projectNumber, "item", label)
		return ""
	}

	gql, err := p.graphqlClient(ctx)
	if err != nil {
		p.log.WarnContext(ctx, "intake: project item skipped: graphql client", "error", err)
		return ""
	}
	var m struct {
		AddProjectV2ItemById struct {
			Item struct {
				ID string `graphql:"id"`
			} `graphql:"item"`
		} `graphql:"addProjectV2ItemById(input: $input)"`
	}
	input := githubv4.AddProjectV2ItemByIdInput{
		ProjectID: githubv4.ID(boardID),
		ContentID: githubv4.ID(contentNodeID),
	}
	if err := gql.Mutate(ctx, &m, input, nil); err != nil {
		p.log.WarnContext(ctx, "intake: addProjectV2ItemById failed (fail-soft)",
			"item", label, "project_number", p.projectNumber, "error", err)
		return ""
	}
	return m.AddProjectV2ItemById.Item.ID
}

// mirrorStateMutation writes the State single-select value for one board
// item. The mutation reaches the endpoint even with empty ids (see the
// caller's note); the error it produces is the operator-visible signal.
func (p *Projector) mirrorStateMutation(ctx context.Context, itemID, fieldID, optID, state string) error {
	p.stateMu.Lock()
	boardID := p.boardNodeID
	p.stateMu.Unlock()

	gql, err := p.graphqlClient(ctx)
	if err != nil {
		return err
	}
	var m struct {
		UpdateProjectV2ItemFieldValue struct {
			Item struct {
				ID string `graphql:"id"`
			} `graphql:"item"`
		} `graphql:"updateProjectV2ItemFieldValue(input: $input)"`
	}
	input := githubv4.UpdateProjectV2ItemFieldValueInput{
		ProjectID: githubv4.ID(boardID),
		ItemID:    githubv4.ID(itemID),
		FieldID:   githubv4.ID(fieldID),
		Value: githubv4.ProjectV2FieldValue{
			SingleSelectOptionID: githubv4.NewString(githubv4.String(optID)),
		},
	}
	if err := gql.Mutate(ctx, &m, input, nil); err != nil {
		return fmt.Errorf("updateProjectV2ItemFieldValue(state=%s): %w", state, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// REST: sub-issue creation through the go-github escape hatch.
// ---------------------------------------------------------------------------

// subIssueCreate is the POST body for the sub-issues route. See the file
// header for the recorded deviation from GitHub's live body shape.
type subIssueCreate struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// createSubIssue creates an issue that is already parented to parentNumber,
// in one POST. Returns the new issue number and node id.
func (p *Projector) createSubIssue(ctx context.Context, parentNumber int, title, body string) (int, string, error) {
	if parentNumber <= 0 {
		return 0, "", fmt.Errorf("parent issue number %d is not positive", parentNumber)
	}
	client, err := p.pool.For(ctx, p.repo.Owner, p.repo.Repo)
	if err != nil {
		return 0, "", fmt.Errorf("pool client: %w", err)
	}
	path := fmt.Sprintf("repos/%s/%s/issues/%d/sub_issues", p.repo.Owner, p.repo.Repo, parentNumber)
	req, err := client.NewRequest(ctx, http.MethodPost, path, subIssueCreate{Title: title, Body: body})
	if err != nil {
		return 0, "", fmt.Errorf("build sub-issue request: %w", err)
	}
	var issue github.Issue
	if _, err := client.Do(req, &issue); err != nil {
		return 0, "", fmt.Errorf("POST %s: %w", path, err)
	}
	if issue.GetNumber() == 0 {
		return 0, "", fmt.Errorf("POST %s returned issue number 0", path)
	}
	return issue.GetNumber(), issue.GetNodeID(), nil
}

// ---------------------------------------------------------------------------
// Titles, bodies, ids (Contract 5's frozen shapes).
// ---------------------------------------------------------------------------

func phaseTitle(phase plan.PlanPhase) string {
	return fmt.Sprintf("phase:%d %s", phase.Sequence, phase.Name)
}

func phaseBody(planID string, phase plan.PlanPhase) string {
	return fmt.Sprintf("meept-plan:%s phase:%s\n%s", planID, phase.ID,
		phaseSummary(phase))
}

func phaseSummary(phase plan.PlanPhase) string {
	return fmt.Sprintf("Phase %d of %d declared steps (state: %s).",
		phase.Sequence, phase.TotalSteps, string(phase.State))
}

func stepTitle(seq int, description, stepID string) string {
	desc := strings.TrimSpace(description)
	if desc == "" {
		desc = stepID
	}
	return fmt.Sprintf("step:%d %s", seq, truncateRunes(desc, maxStepTitleRunes))
}

func stepBody(planID, phaseID, stepID string, step task.TaskStep, titles map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "meept-plan:%s phase:%s step:%s\n", planID, phaseID, stepID)
	if len(step.DependsOn) > 0 {
		names := make([]string, 0, len(step.DependsOn))
		for _, dep := range step.DependsOn {
			name := titles[dep]
			if name == "" {
				name = dep
			}
			names = append(names, name)
		}
		fmt.Fprintf(&b, "Depends on: %s\n", strings.Join(names, ", "))
	}
	return b.String()
}

// synthesizedStepID derives a stable step id for the TotalSteps fallback.
// It is content-derived (phase id + sequence), so a replay claims the same
// id; no clock and no math/rand (the predictable-ID rule).
func synthesizedStepID(phaseID string, seq int) string {
	return fmt.Sprintf("%s-s%d", phaseID, seq)
}

func unmarshalReceipt(raw []byte) (*projectionReceipt, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("projection receipt is empty")
	}
	var r projectionReceipt
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("unmarshal projection receipt: %w", err)
	}
	if r.PhaseIssue == 0 {
		return nil, fmt.Errorf("projection receipt carries no phase issue number")
	}
	return &r, nil
}
