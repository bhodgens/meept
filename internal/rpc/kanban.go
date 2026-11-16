package rpc

// kanban.go — the C4 kanban.* RPC surface (plan tree 05/01). Thirteen frozen
// method strings from master.md C4, served as a registerable unit over narrow
// interfaces into the committed internal/kanban service layer
// (Manager/HumanCommands/Reconciler/Store). LIVE daemon registration
// (calling RegisterKanbanMethods from the wiring) is leaf 07/01 —
// internal/daemon/kanban_wiring.go registers the methods on the running
// daemon when [kanban].enabled (default false: the methods are not
// served, byte-identical to a pre-kanban daemon).
//
// Layering (master.md "no logic duplication" review check): this layer ONLY
// decodes params, enforces the C5 grant lane, delegates, and serialises. All
// lease/WIP/timeline/move semantics live in internal/kanban (HumanCommands /
// Manager); the sidecar gateway (internal/kanbansvc) stays a 401/403 REST
// front that forwards INTO these methods, never a re-implementation.
//
// Param-name contract (leaf 05/01 alignment, orchestrator-verified): the
// gateway converts REST query/body keys DIRECTLY into these RPC params
// (gateway.go restRoutes + buildParams), so the keys the already-tested lanes
// send ARE the wire contract:
//
//	method                 CLI (cmd/meept/kanban.go)      Dart (kanban_service.dart)   TUI (kanban_view.go)      handler accepts (precedence left→right)
//	kanban.board.list      {}                             —                            nil                       —
//	kanban.board.create    {name, id?}                    {name}                       —                         name, id
//	kanban.column.list     {board_id}                     ?board_id=                   {board_id}                board_id
//	kanban.column.set_wip  {column_id, wip, board_id?}    {column_id, wip}             —                         column_id; wip>limit; board_id optional (column lookup)
//	kanban.card.list       {column_id, limit?}            ?board_id&column_id&limit    ?board_id                 column_id | board_id-only aggregate; limit int-or-string
//	kanban.card.get        {card_id}                      ?card_id=                    {card_id}                 card_id
//	kanban.card.create     {board_id, column_id, title, labels?}  +order               —                         same
//	kanban.card.move       {card_id, column_id, order?}   {card_id, column_id, order?, actor?}  {card_id, column_id, actor}  target: column_id > to_column_id > to_column(legacy); from_column_id > from_column(legacy)
//	kanban.card.takeover   {card_id, column_id?}          {card_id, to_column_id?, actor?}      {card_id, actor, to_column_id?}  target: column_id (CLI primary) > to_column_id (Dart/TUI) > to_column (legacy)
//	kanban.card.assign     {card_id, employee_id}         {card_id, employee_id}       same                      card_id, employee_id
//	kanban.card.comment    {card_id, body}                {card_id, body}              {card_id, body}           body > text (legacy)
//	kanban.card.timeline   {card_id}                      ?card_id=                    {card_id}                 card_id
//	kanban.reconcile.now   {}                             {}                           {}                        —
//
// Where CLI and Dart disagree with EACH OTHER (takeover target column_id vs
// to_column_id), the CLI is primary for meept and the handler accepts BOTH
// with the documented precedence; parity leaf 05/06 normalises the lanes.
// Decoding is TOLERANT of unknown keys (forwarded proxies must not 400 on a
// key another lane adds); required-field checks are explicit per handler.
//
// Reply shape: successes ride the ok-envelope {"ok":true, …payload}.
// Business refusals (lease held, WIP full, unknown card/column, stale card,
// missing grant) and parameter errors are returned as Go errors carrying the
// FROZEN C1 error text ("lease held by <holder> since <RFC3339>", "WIP limit
// reached for column <id>: <limit>", "unknown card: <id>", "missing grant
// <g>"). All three surfaces parse refusals from the transport-error path with
// regexes over exactly those strings (CLI kanbanError, TUI
// kanbanRefusalMessage, Dart mapStatusError/_parseLeaseHeld/_parseWipFull
// behind the gateway's 502) — a success-body envelope would make the TUI and
// the Flutter bridge treat a refusal as success. Parameter errors wrap
// ErrKanbanInvalidParams (registered with errcls) so server.dispatch maps
// them to JSON-RPC -32602 InvalidParams.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/errcls"
	"github.com/caimlas/meept/internal/kanban"
	"github.com/caimlas/meept/pkg/id"
)

// ErrKanbanInvalidParams marks the kanban surface's parameter errors so
// server.dispatch answers -32602 (JSON-RPC InvalidParams) instead of -32603.
var ErrKanbanInvalidParams = errors.New("invalid params")

func init() { errcls.RegisterParameterSentinels(ErrKanbanInvalidParams) }

// ---- param decoding helpers -------------------------------------------------

// firstNonEmpty returns the first non-empty candidate (documented key
// precedence resolver).
func firstNonEmpty(candidates ...string) string {
	for _, c := range candidates {
		if c != "" {
			return c
		}
	}
	return ""
}

// kanbanFlexInt accepts a JSON number OR the decimal-string form the gateway
// produces when a value rode the query string (buildParams forwards
// map[string]string verbatim, so the Dart bridge's limit arrives as "5" while
// the CLI's Unix-socket call arrives as 5). Both must decode identically.
type kanbanFlexInt int

func (k *kanbanFlexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*k = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("kanban: expected a number, got %s", b)
	}
	*k = kanbanFlexInt(n)
	return nil
}

// KanbanHandler serves the C4 surface. Build with NewKanbanHandler, register
// with RegisterKanbanMethods.
type KanbanHandler struct {
	deps KanbanDeps
}

// Compile-time proof the live service types satisfy the seams (leaf 07/01
// wires these exact concrete instances).
var (
	_ KanbanBoardStore    = (*kanban.Store)(nil)
	_ KanbanBoardManager  = (*kanban.Manager)(nil)
	_ KanbanHumanLane     = (*kanban.HumanCommands)(nil)
	_ KanbanReconcileLane = (*kanban.Reconciler)(nil)
	_ KanbanGrantGate     = (*kanban.Authorizer)(nil)
)

// NewKanbanHandler builds the handler over the injected seams.
func NewKanbanHandler(deps KanbanDeps) *KanbanHandler {
	return &KanbanHandler{deps: deps}
}

// RegisterKanbanMethods registers all thirteen C4 methods on server. Same
// shape as RegisterIntakeStatusMethods; a nil server is a no-op.
func (h *KanbanHandler) RegisterKanbanMethods(server *Server) {
	if server == nil {
		return
	}
	server.RegisterHandler("kanban.board.list", h.handleBoardList)
	server.RegisterHandler("kanban.board.create", h.handleBoardCreate)
	server.RegisterHandler("kanban.column.list", h.handleColumnList)
	server.RegisterHandler("kanban.column.set_wip", h.handleColumnSetWIP)
	server.RegisterHandler("kanban.card.list", h.handleCardList)
	server.RegisterHandler("kanban.card.get", h.handleCardGet)
	server.RegisterHandler("kanban.card.create", h.handleCardCreate)
	server.RegisterHandler("kanban.card.move", h.handleCardMove)
	server.RegisterHandler("kanban.card.takeover", h.handleCardTakeover)
	server.RegisterHandler("kanban.card.assign", h.handleCardAssign)
	server.RegisterHandler("kanban.card.comment", h.handleCardComment)
	server.RegisterHandler("kanban.card.timeline", h.handleCardTimeline)
	server.RegisterHandler("kanban.reconcile.now", h.handleReconcileNow)
}

// depsRef is the nil-safe accessor: a nil handler degrades every call to an
// "not wired" error (unwired deps), never a panic (intake_status posture).
func (h *KanbanHandler) depsRef() KanbanDeps {
	if h == nil {
		return KanbanDeps{}
	}
	return h.deps
}

// ---- authz lane -----------------------------------------------------------

// methodGrants is the C5 lane per C4 method. Multiple grants on one method
// are CONJUNCTIVE (all required) — same semantics the sidecar gateway
// enforces at REST level for takeover (internal/kanbansvc/gateway.go,
// 310701d5).
var methodGrants = map[string][]string{
	"kanban.board.list":     {kanban.GrantBoardRead},
	"kanban.board.create":   {kanban.GrantBoardMutate},
	"kanban.column.list":    {kanban.GrantBoardRead},
	"kanban.column.set_wip": {kanban.GrantBoardMutate},
	"kanban.card.list":      {kanban.GrantBoardRead},
	"kanban.card.get":       {kanban.GrantBoardRead},
	"kanban.card.create":    {kanban.GrantBoardMutate},
	"kanban.card.move":      {kanban.GrantBoardMutate},
	"kanban.card.takeover":  {kanban.GrantBoardMutate, kanban.GrantBoardTakeover},
	"kanban.card.assign":    {kanban.GrantBoardMutate},
	"kanban.card.comment":   {kanban.GrantBoardMutate},
	"kanban.card.timeline":  {kanban.GrantBoardRead},
	"kanban.reconcile.now":  {kanban.GrantBoardMutate},
}

// principalFrom returns the store-validated caller identity for the C5
// check. The sidecar forwards browser calls over the owner Unix socket with
// no attached Identity (gateway.go never injects a principal param), so the
// socket path resolves to "" — owner-trusted by invariant, and
// kanban.Authorizer grants all three C5 grants for any principal when
// multiuser is off. When the HTTP auth bridge attaches an Identity
// (groups.go ContextWithCallerIdentity), its UserID is the principal.
func principalFrom(ctx context.Context) string {
	if idn, ok := CallerIdentityFromContext(ctx); ok && idn != nil {
		return idn.UserID
	}
	return ""
}

// authorize answers the C5 gate for method. A missing grant is returned as
// the error "missing grant <g>" — the CLI kanbanError and the gateway/Dart
// 403 lane both name the grant by parsing that text.
func (h *KanbanHandler) authorize(ctx context.Context, method string) error {
	grants := methodGrants[method]
	// nil Authorizer = owner-trusted socket (see principalFrom). An
	// attached identity with no wired gate still cannot be more privileged
	// than the socket owner, so allow.
	if h == nil || h.depsRef().Authorizer == nil {
		return nil
	}
	principal := principalFrom(ctx)
	for _, grant := range grants {
		if !h.depsRef().Authorizer.CheckGrant(principal, grant) {
			return fmt.Errorf("missing grant %s", grant)
		}
	}
	return nil
}

// resolveActor picks the audit actor for a human mutation: explicit params
// actor, then the store-validated identity name, then the socket owner
// sentinel. HumanCommands refuses an empty actor, so the fallback chain
// must never yield "".
func resolveActor(ctx context.Context, paramsActor string) string {
	if paramsActor != "" {
		return paramsActor
	}
	if idn, ok := CallerIdentityFromContext(ctx); ok && idn != nil {
		if idn.UserName != "" {
			return idn.UserName
		}
		if idn.UserID != "" {
			return idn.UserID
		}
	}
	return "owner"
}

// ---- envelope + error helpers ----------------------------------------------

func kanbanOK() map[string]any { return map[string]any{"ok": true} }

func kanbanParamsErr(msg string) error {
	return fmt.Errorf("%w: %s", ErrKanbanInvalidParams, msg)
}

// decodeParams unmarshals the request body into dst, TOLERANT of unknown
// keys (a forwarded proxy must not reject a key another lane adds). Empty
// params (a legal GET-forwarded read with no query values) decodes as the
// zero struct. Malformed JSON is an InvalidParams error.
func decodeParams(params json.RawMessage, dst any) error {
	if len(params) == 0 || string(params) == "null" {
		return nil
	}
	if err := json.Unmarshal(params, dst); err != nil {
		return kanbanParamsErr(err.Error())
	}
	return nil
}

// ---- wire DTOs (C1 structs carry no JSON tags; these pin the snake_case
// wire contract the CLI renderers (kanban_format.go getStringOr calls), the
// TUI wire readers (kanban_view_wire.go kanbanFieldStr) and the Dart models
// (kanban_models.dart _pick) actually read. Every key below is pinned by a
// test against the reader-key lists from those three surfaces.) --------------------

func boardDTO(b kanban.Board) map[string]any {
	return map[string]any{"id": b.ID, "name": b.Name}
}

func columnDTO(c kanban.Column) map[string]any {
	return map[string]any{
		"id": c.ID, "board_id": c.BoardID, "name": c.Name,
		"position": c.Position, "wip": c.WIP,
	}
}

func cardDTO(c kanban.Card) map[string]any {
	labels := c.Labels
	if labels == nil {
		labels = []string{}
	}
	return map[string]any{
		"id": c.ID, "board_id": c.BoardID, "column_id": c.ColumnID,
		"title":   c.Title,
		"ref":     map[string]any{"kind": c.Ref.Kind, "id": c.Ref.ID},
		"labels":  labels,
		"goal_id": c.GoalID, "employee_id": c.EmployeeID,
		"plan_id": c.PlanID, "session_id": c.SessionID,
		"trigger_ref": c.TriggerRef, "order": c.Order,
	}
}

// cardEventDTO pins the timeline-row keys: all three surfaces read
// created_at (CLI renderTimeline / TUI Timestamp / Dart KanbanCardEvent
// createdAt), kind, actor, body — NOT "created_at_rfc3339", which was the
// pre-alignment handler coinage no reader consumes. The value is the
// RFC3339 string (DateTime.tryParse + time.Parse both take it).
func cardEventDTO(ev kanban.CardEvent) map[string]any {
	return map[string]any{
		"id": ev.ID, "card_id": ev.CardID, "kind": ev.Kind,
		"actor": ev.Actor, "body": ev.Body,
		"created_at": ev.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func cardDTOs(cs []kanban.Card) []map[string]any {
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, cardDTO(c))
	}
	return out
}

// ---- read handlers --------------------------------------------------------

type kanbanBoardIDParams struct {
	BoardID string `json:"board_id"`
}

type kanbanCardIDParams struct {
	CardID string `json:"card_id"`
}

// handleBoardList serves kanban.board.list (no params).
func (h *KanbanHandler) handleBoardList(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.authorize(ctx, "kanban.board.list"); err != nil {
		return nil, err
	}
	if h.depsRef().Store == nil {
		return nil, errors.New("kanban rpc: store not wired")
	}
	boards, err := h.depsRef().Store.ListBoards(ctx)
	if err != nil {
		return nil, err
	}
	out := kanbanOK()
	dtos := make([]map[string]any, 0, len(boards))
	for _, b := range boards {
		dtos = append(dtos, boardDTO(b))
	}
	out["boards"] = dtos
	return out, nil
}

// handleColumnList serves kanban.column.list {board_id}.
func (h *KanbanHandler) handleColumnList(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.authorize(ctx, "kanban.column.list"); err != nil {
		return nil, err
	}
	var p kanbanBoardIDParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.BoardID == "" {
		return nil, kanbanParamsErr("board_id is required")
	}
	if h.depsRef().Store == nil {
		return nil, errors.New("kanban rpc: store not wired")
	}
	columns, err := h.depsRef().Store.ListColumns(ctx, p.BoardID)
	if err != nil {
		return nil, err
	}
	out := kanbanOK()
	dtos := make([]map[string]any, 0, len(columns))
	for _, c := range columns {
		dtos = append(dtos, columnDTO(c))
	}
	out["columns"] = dtos
	return out, nil
}

// kanbanCardListParams: card.list. The CLI keys on column_id; the Dart
// bridge may send board_id and/or column_id (query passthrough) and the TUI
// fetches the whole board's cards with board_id alone. limit is the
// RPC-side pagination knob (int from the CLI, decimal string when it rode
// the gateway query string) — unrelated to set_wip's wip knob.
type kanbanCardListParams struct {
	ColumnID string         `json:"column_id"`
	BoardID  string         `json:"board_id"`
	Limit    *kanbanFlexInt `json:"limit"`
}

// handleCardList serves kanban.card.list {column_id} or the board-scoped
// {board_id} form (aggregates the board's columns in position order).
func (h *KanbanHandler) handleCardList(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.authorize(ctx, "kanban.card.list"); err != nil {
		return nil, err
	}
	var p kanbanCardListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ColumnID == "" && p.BoardID == "" {
		return nil, kanbanParamsErr("column_id or board_id is required")
	}
	if h.depsRef().Store == nil {
		return nil, errors.New("kanban rpc: store not wired")
	}
	var cards []kanban.Card
	if p.ColumnID != "" {
		var err error
		cards, err = h.depsRef().Store.ListCards(ctx, p.ColumnID)
		if err != nil {
			return nil, err
		}
	} else {
		// board-only view (TUI fetchCards, Dart listCards(boardId:…)):
		// ListCards is column-scoped in the store, so walk the board's
		// columns in position order and concatenate.
		columns, err := h.depsRef().Store.ListColumns(ctx, p.BoardID)
		if err != nil {
			return nil, err
		}
		for _, col := range columns {
			colCards, err := h.depsRef().Store.ListCards(ctx, col.ID)
			if err != nil {
				return nil, err
			}
			cards = append(cards, colCards...)
		}
	}
	if p.Limit != nil && int(*p.Limit) >= 0 && int(*p.Limit) < len(cards) {
		cards = cards[:int(*p.Limit)]
	}
	out := kanbanOK()
	out["cards"] = cardDTOs(cards)
	return out, nil
}

// handleCardGet serves kanban.card.get {card_id}. An unknown card answers
// the C1 ErrUnknownCard error text.
func (h *KanbanHandler) handleCardGet(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.authorize(ctx, "kanban.card.get"); err != nil {
		return nil, err
	}
	var p kanbanCardIDParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.CardID == "" {
		return nil, kanbanParamsErr("card_id is required")
	}
	if h.depsRef().Store == nil {
		return nil, errors.New("kanban rpc: store not wired")
	}
	card, err := h.depsRef().Store.GetCard(ctx, p.CardID)
	if err != nil {
		return nil, err
	}
	out := kanbanOK()
	out["card"] = cardDTO(card)
	return out, nil
}

// handleCardTimeline serves kanban.card.timeline {card_id} (C3/C4: the
// append-only card_events rows, oldest-first — HumanCommands owns the read).
func (h *KanbanHandler) handleCardTimeline(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.authorize(ctx, "kanban.card.timeline"); err != nil {
		return nil, err
	}
	var p kanbanCardIDParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.CardID == "" {
		return nil, kanbanParamsErr("card_id is required")
	}
	if h.depsRef().Human == nil {
		return nil, errors.New("kanban rpc: human lane not wired")
	}
	events, err := h.depsRef().Human.Timeline(ctx, p.CardID)
	if err != nil {
		return nil, err
	}
	out := kanbanOK()
	dtos := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		dtos = append(dtos, cardEventDTO(ev))
	}
	out["events"] = dtos
	return out, nil
}

// ---- mutation handlers ------------------------------------------------------

type kanbanBoardCreateParams struct {
	Name string `json:"name"`
	ID   string `json:"id,omitempty"`
}

// handleBoardCreate serves kanban.board.create {name, id?}. The store mints
// a missing id; this layer mints it first so the reply can echo it (the
// store takes Board by value).
func (h *KanbanHandler) handleBoardCreate(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.authorize(ctx, "kanban.board.create"); err != nil {
		return nil, err
	}
	var p kanbanBoardCreateParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Name == "" {
		return nil, kanbanParamsErr("name is required")
	}
	if h.depsRef().Store == nil {
		return nil, errors.New("kanban rpc: store not wired")
	}
	b := kanban.Board{ID: p.ID, Name: p.Name}
	if b.ID == "" {
		b.ID = id.Generate("board-")
	}
	if err := h.depsRef().Store.CreateBoard(ctx, b); err != nil {
		return nil, err
	}
	out := kanbanOK()
	out["board"] = boardDTO(b)
	return out, nil
}

// kanbanSetWIPParams: set_wip. The CLI (`meept kanban wip <column_id>
// <limit>`) and the Dart bridge both send {column_id, wip} and send
// board_id ONLY with --board; "limit" is the pre-alignment handler coinage
// kept as a last-resort alias. Int-or-string for the gateway passthrough.
type kanbanSetWIPParams struct {
	BoardID  string         `json:"board_id"`
	ColumnID string         `json:"column_id"`
	WIP      *kanbanFlexInt `json:"wip"`
	Limit    *kanbanFlexInt `json:"limit"` // legacy alias, lowest precedence
}

// handleColumnSetWIP serves kanban.column.set_wip {column_id, wip, board_id?}.
// wip 0 = no limit; Manager owns the normalisation. board_id is resolved
// from the column when the caller (the default CLI and every Dart call)
// omits it — Manager.SetWIPLimit and the store scope the write per board.
func (h *KanbanHandler) handleColumnSetWIP(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.authorize(ctx, "kanban.column.set_wip"); err != nil {
		return nil, err
	}
	var p kanbanSetWIPParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ColumnID == "" {
		return nil, kanbanParamsErr("column_id is required")
	}
	wip := p.WIP
	if wip == nil {
		wip = p.Limit
	}
	if wip == nil {
		return nil, kanbanParamsErr("wip is required")
	}
	if h.depsRef().Store == nil {
		return nil, errors.New("kanban rpc: store not wired")
	}
	if h.depsRef().Manager == nil {
		return nil, errors.New("kanban rpc: manager not wired")
	}
	boardID := p.BoardID
	if boardID == "" {
		resolved, err := h.boardForColumn(ctx, p.ColumnID)
		if err != nil {
			return nil, err
		}
		boardID = resolved
	}
	if err := h.depsRef().Manager.SetWIPLimit(ctx, kanban.Board{ID: boardID}, p.ColumnID, int(*wip)); err != nil {
		return nil, err
	}
	return kanbanOK(), nil
}

// boardForColumn resolves the board owning columnID via ListBoards +
// ListColumns (no GetColumn exists on the store; an RPC-layer scan is the
// cheapest read-only way to honour the board-less CLI/Dart payloads).
func (h *KanbanHandler) boardForColumn(ctx context.Context, columnID string) (string, error) {
	boards, err := h.depsRef().Store.ListBoards(ctx)
	if err != nil {
		return "", err
	}
	for _, b := range boards {
		columns, err := h.depsRef().Store.ListColumns(ctx, b.ID)
		if err != nil {
			return "", err
		}
		for _, c := range columns {
			if c.ID == columnID {
				return b.ID, nil
			}
		}
	}
	return "", kanban.ErrUnknownColumn{ID: columnID}
}

type kanbanCardCreateParams struct {
	BoardID    string   `json:"board_id"`
	ColumnID   string   `json:"column_id"`
	Title      string   `json:"title"`
	ID         string   `json:"id,omitempty"`
	RefKind    string   `json:"ref_kind,omitempty"`
	RefID      string   `json:"ref_id,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	GoalID     string   `json:"goal_id,omitempty"`
	EmployeeID string   `json:"employee_id,omitempty"`
	PlanID     string   `json:"plan_id,omitempty"`
	SessionID  string   `json:"session_id,omitempty"`
	TriggerRef string   `json:"trigger_ref,omitempty"`
	Order      int      `json:"order,omitempty"`
}

// handleCardCreate serves kanban.card.create {board_id, column_id, title,
// …}. Provenance fields ride through untouched (C1); the store owns FK
// enforcement and id minting.
func (h *KanbanHandler) handleCardCreate(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.authorize(ctx, "kanban.card.create"); err != nil {
		return nil, err
	}
	var p kanbanCardCreateParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.BoardID == "" || p.ColumnID == "" || p.Title == "" {
		return nil, kanbanParamsErr("board_id, column_id and title are required")
	}
	if h.depsRef().Store == nil {
		return nil, errors.New("kanban rpc: store not wired")
	}
	c := kanban.Card{
		ID: p.ID, BoardID: p.BoardID, ColumnID: p.ColumnID, Title: p.Title,
		Ref:    kanban.WorkRef{Kind: p.RefKind, ID: p.RefID},
		Labels: p.Labels,
		GoalID: p.GoalID, EmployeeID: p.EmployeeID,
		PlanID: p.PlanID, SessionID: p.SessionID, TriggerRef: p.TriggerRef,
		Order: p.Order,
	}
	if c.ID == "" {
		c.ID = id.Generate("card-")
	}
	if err := h.depsRef().Store.CreateCard(ctx, c); err != nil {
		return nil, err
	}
	out := kanbanOK()
	out["card"] = cardDTO(c)
	return out, nil
}

// kanbanCardMoveParams: move. Every lane (CLI, Dart, TUI) names the target
// column_id; to_column_id is accepted for takeover-parity payloads and
// to_column is the pre-alignment handler coinage (legacy alias, lowest
// precedence). order is explicit-only (pointer): CLI sends it only with
// --order, Dart only when non-null.
type kanbanCardMoveParams struct {
	CardID       string         `json:"card_id"`
	ColumnID     string         `json:"column_id"`
	ToColumnID   string         `json:"to_column_id"`
	ToColumn     string         `json:"to_column"` // legacy alias
	FromColumnID string         `json:"from_column_id"`
	FromColumn   string         `json:"from_column"` // legacy alias
	Order        *kanbanFlexInt `json:"order"`
	Actor        string         `json:"actor"`
}

// handleCardMove serves kanban.card.move {card_id, column_id, order?,
// actor?}. MoveCard owns lease rule A (C10) and the WIP rule; refusals
// surface as the frozen C1 error texts. An explicit order is applied after
// the move (HumanCommands has no order verb; the store CAS does), including
// the same-column ordering gesture, which move answers as a legal no-op.
func (h *KanbanHandler) handleCardMove(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.authorize(ctx, "kanban.card.move"); err != nil {
		return nil, err
	}
	var p kanbanCardMoveParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	target := firstNonEmpty(p.ColumnID, p.ToColumnID, p.ToColumn)
	from := firstNonEmpty(p.FromColumnID, p.FromColumn)
	if p.CardID == "" || target == "" {
		return nil, kanbanParamsErr("card_id and column_id are required")
	}
	if h.depsRef().Human == nil {
		return nil, errors.New("kanban rpc: human lane not wired")
	}
	if err := h.depsRef().Human.MoveCard(ctx, p.CardID, from, target, resolveActor(ctx, p.Actor)); err != nil {
		return nil, err
	}
	if p.Order != nil {
		if err := h.setCardOrder(ctx, p.CardID, int(*p.Order)); err != nil {
			return nil, err
		}
	}
	return kanbanOK(), nil
}

// setCardOrder applies an explicit ordering hint post-move via the store
// read-modify-write. GetCard failure propagates: a card deleted mid-move is
// a stale write the caller must see, not silence.
func (h *KanbanHandler) setCardOrder(ctx context.Context, cardID string, order int) error {
	if h.depsRef().Store == nil {
		return errors.New("kanban rpc: store not wired")
	}
	card, err := h.depsRef().Store.GetCard(ctx, cardID)
	if err != nil {
		return err
	}
	if card.Order == order {
		return nil
	}
	card.Order = order
	return h.depsRef().Store.UpdateCard(ctx, card)
}

// kanbanCardTakeoverParams: takeover. The CLI sends the target as
// column_id (primary for meept), the Dart bridge and the TUI send
// to_column_id; both are accepted, column_id wins when both appear.
// to_column is the legacy coinage. Parity leaf 05/06 normalises the lanes.
type kanbanCardTakeoverParams struct {
	CardID     string `json:"card_id"`
	ColumnID   string `json:"column_id"`
	ToColumnID string `json:"to_column_id"`
	ToColumn   string `json:"to_column"` // legacy alias
	Actor      string `json:"actor"`
}

// handleCardTakeover serves kanban.card.takeover {card_id, column_id?,
// actor?} under the CONJUNCTIVE gate (mutate AND takeover). Mapping choice,
// pinned to master.md: decision 1 and C10 both describe takeover as
// "cancels the episode, releases the token, audits, THEN moves" — the full
// gesture is HumanCommands.TakeoverMove, so a request carrying a target
// column routes there; its reply stamps lease_blocked:true (the bypass flag
// the CLI renderMutation surfaces). A request without a target lands on
// bare Takeover (audit + lease break, no move): C10 calls takeover "the only
// bypass" and the CLI leaf shows `meept kanban takeover <card_id>` with no
// column — takeover must stay legal standalone.
func (h *KanbanHandler) handleCardTakeover(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.authorize(ctx, "kanban.card.takeover"); err != nil {
		return nil, err
	}
	var p kanbanCardTakeoverParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.CardID == "" {
		return nil, kanbanParamsErr("card_id is required")
	}
	if h.depsRef().Human == nil {
		return nil, errors.New("kanban rpc: human lane not wired")
	}
	target := firstNonEmpty(p.ColumnID, p.ToColumnID, p.ToColumn)
	actor := resolveActor(ctx, p.Actor)
	var err error
	out := kanbanOK()
	if target != "" {
		err = h.depsRef().Human.TakeoverMove(ctx, p.CardID, "", target, actor)
		out["lease_blocked"] = true
	} else {
		err = h.depsRef().Human.Takeover(ctx, p.CardID, actor)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

type kanbanCardAssignParams struct {
	CardID     string `json:"card_id"`
	EmployeeID string `json:"employee_id"`
}

// handleCardAssign serves kanban.card.assign {card_id, employee_id} (all
// lanes agree on both keys). employee_id "" un-assigns
// (HumanCommands.Assign contract).
func (h *KanbanHandler) handleCardAssign(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.authorize(ctx, "kanban.card.assign"); err != nil {
		return nil, err
	}
	var p kanbanCardAssignParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.CardID == "" {
		return nil, kanbanParamsErr("card_id is required")
	}
	if h.depsRef().Human == nil {
		return nil, errors.New("kanban rpc: human lane not wired")
	}
	if err := h.depsRef().Human.Assign(ctx, p.CardID, p.EmployeeID); err != nil {
		return nil, err
	}
	return kanbanOK(), nil
}

// kanbanCardCommentParams: comment. CLI, Dart and TUI all send body;
// "text" is the pre-alignment handler coinage kept as a legacy alias.
type kanbanCardCommentParams struct {
	CardID string `json:"card_id"`
	Body   string `json:"body"`
	Text   string `json:"text"` // legacy alias
	Actor  string `json:"actor"`
}

// handleCardComment serves kanban.card.comment {card_id, body, actor?}; the
// appended CardEvent echoes back (round-trips against card.timeline).
func (h *KanbanHandler) handleCardComment(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.authorize(ctx, "kanban.card.comment"); err != nil {
		return nil, err
	}
	var p kanbanCardCommentParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	body := firstNonEmpty(p.Body, p.Text)
	if p.CardID == "" || body == "" {
		return nil, kanbanParamsErr("card_id and body are required")
	}
	if h.depsRef().Human == nil {
		return nil, errors.New("kanban rpc: human lane not wired")
	}
	ev, err := h.depsRef().Human.Comment(ctx, p.CardID, resolveActor(ctx, p.Actor), body)
	if err != nil {
		return nil, err
	}
	out := kanbanOK()
	out["event"] = cardEventDTO(ev)
	return out, nil
}

// handleReconcileNow serves kanban.reconcile.now {} (no params; the gateway
// forwards an empty body as null). Emits the per-pass ReconcileStats
// counters FLAT at the envelope top level (created/moved/fixed/deleted +
// total) — that is the shape both readers were tested against: the CLI
// renderReconcile reads them from the top level (or under "stats") and the
// Dart reconcileNow hands the whole body to KanbanReconcileStats.fromJson.
// The rebuild logic lives entirely in the Reconciler.
func (h *KanbanHandler) handleReconcileNow(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.authorize(ctx, "kanban.reconcile.now"); err != nil {
		return nil, err
	}
	if h.depsRef().Reconciler == nil {
		return nil, errors.New("kanban rpc: reconciler not wired")
	}
	stats, err := h.depsRef().Reconciler.ReconcileNowStats(ctx)
	if err != nil {
		return nil, err
	}
	out := kanbanOK()
	out["created"] = stats.Created
	out["moved"] = stats.Moved
	out["fixed"] = stats.Fixed
	out["deleted"] = stats.Deleted
	out["total"] = stats.Total()
	return out, nil
}
