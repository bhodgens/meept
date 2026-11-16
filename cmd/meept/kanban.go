package main

// `meept kanban` — the C6 CLI surface for the kanban board capability
// (plans/kanban-board/master.md C4/C6/C10). Every subcommand speaks the
// kanban.* RPC method set over the daemon transport client (connectDaemon(),
// cmd/meept/main.go) exactly like plans.go / projects.go / effects.go do:
// map params -> client.Call -> tolerant result decode -> tabwriter table or
// --json raw passthrough. C10 human error rendering (lease holder+age, WIP
// column+limit, missing grant) lives in kanbanError.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// kanbanConnectDaemon is the CLI's daemon-dial seam. It points at the
// package-level connectDaemon() in production; kanban_test.go swaps it to
// drive the full RunE -> Call -> render path against a fake
// transport.Client without a live daemon.
var kanbanConnectDaemon = connectDaemon

// newKanbanCmd builds the `meept kanban` command tree (C6).
func newKanbanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "kanban",
		Short:         "inspect and drive the kanban board (cards over work items)",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `Kanban board: one canonical card per work item, projected from the
plan/task/step/goal/job machines. Board facts (columns, order, labels,
notes) live in the store; card state is rebuilt on demand from source.

Verbs (C6):
  meept kanban list                              boards
  meept kanban show <board_id>                   board view (--card for one card)
  meept kanban board create|list|columns|cards   board/column/card reads + board create
  meept kanban create <board_id> <column_id> <title>   create a card
  meept kanban move <card_id> <column_id>        move a card (lease rule enforced)
  meept kanban takeover <card_id>                cancel the episode, release the lease, move
  meept kanban wip <column_id> <limit>           set the column wip limit (0 = no limit)
  meept kanban assign <card_id> <employee_id>    assign an employee to a card
  meept kanban comment <card_id> <text...>       append a card event
  meept kanban timeline <card_id>                read the card event log
  meept kanban reconcile                         run a projection rebuild pass now`,
	}
	cmd.AddCommand(
		newKanbanListCmd(),
		newKanbanShowCmd(),
		newKanbanBoardCmd(),
		newKanbanCreateCmd(),
		newKanbanMoveCmd(),
		newKanbanTakeoverCmd(),
		newKanbanWIPCmd(),
		newKanbanAssignCmd(),
		newKanbanCommentCmd(),
		newKanbanTimelineCmd(),
		newKanbanReconcileCmd(),
	)
	return cmd
}

// --- board reads -------------------------------------------------------

func newKanbanListCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list boards",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return kanbanInvoke(cmd, "kanban.board.list", map[string]any{}, outputJSON, renderBoards)
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

func newKanbanShowCmd() *cobra.Command {
	var cardID string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "show <board_id>",
		Short: "show a board's columns and cards (or one card with --card)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cardID != "" {
				return kanbanInvoke(cmd, "kanban.card.get", map[string]any{"card_id": cardID}, outputJSON, renderCardResult)
			}
			return runKanbanShowBoard(cmd, args[0], outputJSON)
		},
	}
	cmd.Flags().StringVar(&cardID, "card", "", "show a single card by id instead of the board view")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

// runKanbanShowBoard composes the read path: column.list, then card.list
// per column. A card.list failure on one column degrades to an annotation
// rather than killing the whole view.
func runKanbanShowBoard(cmd *cobra.Command, boardID string, outputJSON bool) error {
	client, err := kanbanConnectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer client.Close()

	colRaw, err := client.Call("kanban.column.list", map[string]any{"board_id": boardID})
	if err != nil {
		return kanbanError(err)
	}
	if outputJSON {
		return printRawJSON(cmd.OutOrStdout(), colRaw)
	}
	cols, err := decodeRows(colRaw, "columns")
	if err != nil {
		return fmt.Errorf("failed to parse columns response: %w", err)
	}

	views := make([]boardColumnView, 0, len(cols))
	for _, col := range cols {
		vc := boardColumnView{Column: col}
		cardRaw, cerr := client.Call("kanban.card.list", map[string]any{"column_id": getStringOr(col, "id", "")})
		if cerr != nil {
			vc.Note = cerr.Error()
		} else if cards, derr := decodeRows(cardRaw, "cards"); derr != nil {
			vc.Note = derr.Error()
		} else {
			vc.Cards = cards
		}
		views = append(views, vc)
	}
	renderBoardView(cmd.OutOrStdout(), boardID, views)
	return nil
}

// --- board tree --------------------------------------------------------

func newKanbanBoardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "board",
		Short:        "board, column, and card subcommands",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
	}
	cmd.AddCommand(
		newKanbanBoardCreateCmd(),
		newKanbanBoardListCmd(),
		newKanbanBoardColumnsCmd(),
		newKanbanBoardCardsCmd(),
	)
	return cmd
}

func newKanbanBoardCreateCmd() *cobra.Command {
	var boardID string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "create a board",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]any{"name": args[0]}
			if boardID != "" {
				params["id"] = boardID
			}
			return kanbanInvoke(cmd, "kanban.board.create", params, outputJSON, renderCreated(fmt.Sprintf("board %q", args[0])))
		},
	}
	cmd.Flags().StringVar(&boardID, "id", "", "explicit board id (daemon generates one when unset)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

func newKanbanBoardListCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list boards",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return kanbanInvoke(cmd, "kanban.board.list", map[string]any{}, outputJSON, renderBoards)
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

func newKanbanBoardColumnsCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "columns <board_id>",
		Short: "list a board's columns",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return kanbanInvoke(cmd, "kanban.column.list", map[string]any{"board_id": args[0]}, outputJSON, renderColumns)
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

func newKanbanBoardCardsCmd() *cobra.Command {
	var outputJSON bool
	var limit int
	cmd := &cobra.Command{
		Use:   "cards <column_id>",
		Short: "list the cards in a column",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]any{"column_id": args[0]}
			if limit > 0 {
				params["limit"] = limit
			}
			return kanbanInvoke(cmd, "kanban.card.list", params, outputJSON, renderCards)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "cap the number of cards returned")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

// --- card mutations ----------------------------------------------------

func newKanbanCreateCmd() *cobra.Command {
	var labels []string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "create <board_id> <column_id> <title>",
		Short: "create a card in a column",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]any{
				"board_id":  args[0],
				"column_id": args[1],
				"title":     args[2],
			}
			if len(labels) > 0 {
				params["labels"] = labels
			}
			return kanbanInvoke(cmd, "kanban.card.create", params, outputJSON, renderCreated(fmt.Sprintf("card %q", args[2])))
		},
	}
	cmd.Flags().StringSliceVar(&labels, "label", nil, "label(s) for the card (repeatable)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

func newKanbanMoveCmd() *cobra.Command {
	var order int
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "move <card_id> <column_id>",
		Short: "move a card to another column (lease rule enforced)",
		Long: `Move a card. A human move onto a column mapped to an active state of
a work item holding a live claim is refused (master.md decision 1); the
error names the holder and its age. Use 'kanban takeover' to bypass.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]any{"card_id": args[0], "column_id": args[1]}
			if cmd.Flags().Changed("order") {
				params["order"] = order
			}
			return kanbanInvoke(cmd, "kanban.card.move", params, outputJSON, renderMutation("card moved"))
		},
	}
	cmd.Flags().IntVar(&order, "order", 0, "manual ordering within the target column")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

func newKanbanTakeoverCmd() *cobra.Command {
	var columnID string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "takeover <card_id>",
		Short: "cancel the episode, release the lease, and move the card",
		Long: `Takeover is the only bypass of the lease rule: it cancels the driving
agent episode, releases the claim token, and audits BEFORE acting. The
daemon requires the board.takeover grant in addition to board.mutate.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]any{"card_id": args[0]}
			if columnID != "" {
				params["column_id"] = columnID
			}
			return kanbanInvoke(cmd, "kanban.card.takeover", params, outputJSON, renderMutation("takeover complete"))
		},
	}
	cmd.Flags().StringVar(&columnID, "column", "", "target column id (daemon default when unset)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

func newKanbanWIPCmd() *cobra.Command {
	var boardID string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "wip <column_id> <limit>",
		Short: "set a column's wip limit (0 = no limit)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("wip limit must be an integer (0 = no limit), got %q", args[1])
			}
			if limit < 0 {
				return fmt.Errorf("wip limit must be >= 0, got %d", limit)
			}
			params := map[string]any{"column_id": args[0], "wip": limit}
			if boardID != "" {
				params["board_id"] = boardID
			}
			return kanbanInvoke(cmd, "kanban.column.set_wip", params, outputJSON, renderMutation(fmt.Sprintf("column %s wip limit set to %d", args[0], limit)))
		},
	}
	cmd.Flags().StringVar(&boardID, "board", "", "board id (include in the request when the daemon needs the scope)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

func newKanbanAssignCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "assign <card_id> <employee_id>",
		Short: "assign an employee to a card",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]any{"card_id": args[0], "employee_id": args[1]}
			return kanbanInvoke(cmd, "kanban.card.assign", params, outputJSON, renderMutation("card assigned"))
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

func newKanbanCommentCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "comment <card_id> <text...>",
		Short: "append a comment event to a card",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]any{"card_id": args[0], "body": strings.Join(args[1:], " ")}
			return kanbanInvoke(cmd, "kanban.card.comment", params, outputJSON, renderMutation("comment added"))
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

func newKanbanTimelineCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "timeline <card_id>",
		Short: "show a card's event timeline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return kanbanInvoke(cmd, "kanban.card.timeline", map[string]any{"card_id": args[0]}, outputJSON, renderTimeline)
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

func newKanbanReconcileCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "run a projection rebuild pass now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return kanbanInvoke(cmd, "kanban.reconcile.now", map[string]any{}, outputJSON, renderReconcile)
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

// --- shared invoke path --------------------------------------------------

// kanbanInvoke is the one round-trip every single-call command uses: dial,
// Call the C4 method, surface handler-level {error: msg} replies as real
// errors, then either passthrough --json or render the table.
func kanbanInvoke(cmd *cobra.Command, method string, params any, outputJSON bool, render func(io.Writer, map[string]any) error) error {
	client, err := kanbanConnectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer client.Close()

	rawResult, err := client.Call(method, params)
	if err != nil {
		return kanbanError(err)
	}
	resultMap, err := decodeResultMap(rawResult)
	if err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	if errMsg := getStringOr(resultMap, "error", ""); errMsg != "" {
		return kanbanError(errors.New(errMsg))
	}
	if outputJSON {
		return printRawJSON(cmd.OutOrStdout(), rawResult)
	}
	if render != nil {
		return render(cmd.OutOrStdout(), resultMap)
	}
	return nil
}

// decodeResultMap accepts both shapes a kanban handler may answer with: a
// JSON object (the intake_status/plans.go convention) or a bare top-level
// array, which is normalised to {"items": [...]} for the renderers.
func decodeResultMap(raw json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err == nil {
		return m, nil
	}
	var arr []any
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, err
	}
	return map[string]any{"items": arr}, nil
}

// decodeRows pulls a row list out of a result map, trying the conventional
// key first ("boards"/"columns"/"cards"/"events") then the bare-array
// normalisation from decodeResultMap.
func decodeRows(raw json.RawMessage, key string) ([]map[string]any, error) {
	m, err := decodeResultMap(raw)
	if err != nil {
		return nil, err
	}
	if errMsg := getStringOr(m, "error", ""); errMsg != "" {
		return nil, errors.New(errMsg)
	}
	for _, k := range []string{key, "items", "rows", "result"} {
		if rows := listMaps(m[k]); rows != nil {
			return rows, nil
		}
	}
	return nil, nil
}

// listMaps coerces []any of objects (or a pre-typed []map[string]any) into
// row maps; nil for anything else. Single-valued by design: callers probe
// nil, and the pre-commit-errors gate forbids the discard idiom.
func listMaps(v any) []map[string]any {
	switch t := v.(type) {
	case []map[string]any:
		return t
	case []any:
		rows := make([]map[string]any, 0, len(t))
		for _, item := range t {
			if m, ok := item.(map[string]any); ok {
				rows = append(rows, m)
			}
		}
		return rows
	default:
		return nil
	}
}

// --- C10 human error rendering -------------------------------------------

var (
	leaseHeldRe = regexp.MustCompile(`(?i)lease held by (\S+)(?: since (\S+))?`)
	wipFullRe   = regexp.MustCompile(`(?i)wip limit reached for (?:column )?([^\s:]+)(?::\s*(\d+))?`)

	// The three C5 grants, in detection order (longest names first so
	// "board.takeover" is never swallowed by a "board.mutate" scan).
	kanbanGrants = []string{"board.takeover", "board.mutate", "board.read"}
)

// kanbanError turns a raw RPC/daemon error into an operator-actionable one,
// honouring C10: ErrLeaseHeld shows holder + age, ErrWIPFull shows
// column + limit, an authz denial names the required grant. Unknown errors
// pass through wrapped so the original text is never lost.
func kanbanError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()

	if m := leaseHeldRe.FindStringSubmatch(msg); m != nil {
		age := "unknown"
		if t, terr := time.Parse(time.RFC3339, m[2]); terr == nil {
			age = humanizeAge(t)
		}
		return fmt.Errorf("move refused: lease held by %s (age %s) — run 'meept kanban takeover <card_id>' to cancel the episode and move", m[1], age)
	}
	if m := wipFullRe.FindStringSubmatch(msg); m != nil {
		col, limit := m[1], strings.TrimSpace(m[2])
		if limit == "" {
			return fmt.Errorf("move refused: wip limit reached in column %s — raise the limit with 'meept kanban wip %s <n>' or clear a card first", col, col)
		}
		return fmt.Errorf("move refused: wip limit reached in column %s (limit %s) — raise it with 'meept kanban wip %s <n+1>' or clear a card first", col, limit, col)
	}
	lower := strings.ToLower(msg)
	looksDenied := strings.Contains(lower, "grant") || strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "not authorized") || strings.Contains(lower, "authorization denied")
	if looksDenied {
		for _, g := range kanbanGrants {
			if strings.Contains(lower, g) {
				return fmt.Errorf("authorization denied: this call requires the '%s' grant (multiuser off: the owner socket always has it)", g)
			}
		}
		if strings.Contains(lower, "grant") {
			return fmt.Errorf("authorization denied: missing grant — %s", msg)
		}
	}
	return fmt.Errorf("kanban rpc failed: %w", err)
}
