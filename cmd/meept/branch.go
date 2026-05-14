package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/caimlas/meept/internal/tui/types"
	"github.com/spf13/cobra"
)

func newBranchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "branch",
		Short: "Manage session branches",
		Long: `List, navigate, and fork conversation branches within sessions.

Branches let you explore alternative responses or fork a conversation
from a prior point without losing the original context.

Examples:
  meept branch list                          # List branches for the most recent session
  meept branch list --session <id>           # List branches for a specific session
  meept branch navigate <message-id>         # Navigate to a prior message, starting a new branch
  meept branch fork <message-id>             # Fork a session from a specific message
  meept branch fork <message-id> --name my-fork  # Fork with a custom name
  meept branch tree                          # Show the conversation tree for the most recent session`,
	}

	cmd.AddCommand(newBranchListCmd())
	cmd.AddCommand(newBranchSummaryCmd())
	cmd.AddCommand(newBranchNavigateCmd())
	cmd.AddCommand(newBranchForkCmd())
	cmd.AddCommand(newBranchTreeCmd())

	return cmd
}

func newBranchListCmd() *cobra.Command {
	var sessionID string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "list branches in a session",
		Long: `List all conversation branches in a session.

Displays an indented tree-like list showing branch names, message counts,
and summary status.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBranchList(sessionID)
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (defaults to most recent)")

	return cmd
}

func newBranchSummaryCmd() *cobra.Command {
	var sessionID string

	cmd := &cobra.Command{
		Use:   "summary",
		Short: "show a summary of each branch in a session",
		Long: `Display a summary of each conversation branch in a session.

Shows branch IDs, message counts, and the full summary text for each
branch (if available). Summaries are generated automatically when
navigating away from a branch with enough abandoned messages.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBranchSummary(sessionID)
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (defaults to most recent)")

	return cmd
}

func newBranchNavigateCmd() *cobra.Command {
	var sessionID string

	cmd := &cobra.Command{
		Use:   "navigate <message-id>",
		Short: "navigate to a prior message, creating a branch",
		Long: `Navigate to a prior message in the conversation, starting a new branch
from that point. The abandoned branch may be automatically summarized.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			messageID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid message id: %s", args[0])
			}
			return runBranchNavigate(sessionID, messageID)
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (defaults to most recent)")

	return cmd
}

func newBranchForkCmd() *cobra.Command {
	var sessionID string
	var forkName string

	cmd := &cobra.Command{
		Use:   "fork <message-id>",
		Short: "fork a session from a specific message",
		Long: `Create a new session by copying messages from root to the specified
message from the source session. The new session can then evolve
independently.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			messageID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid message id: %s", args[0])
			}
			return runBranchFork(sessionID, messageID, forkName)
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (defaults to most recent)")
	cmd.Flags().StringVar(&forkName, "name", "", "Name for the forked session")

	return cmd
}

func newBranchTreeCmd() *cobra.Command {
	var sessionID string

	cmd := &cobra.Command{
		Use:   "tree",
		Short: "show the conversation tree",
		Long:  `Display the full conversation tree for a session, showing message IDs, roles, branch IDs, and leaf markers.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBranchTree(sessionID)
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (defaults to most recent)")

	return cmd
}

func runBranchList(sessionID string) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	if sessionID == "" {
		sessionID, err = resolveMostRecentSessionID(client)
		if err != nil {
			return err
		}
	}

	// Call session.branches.list
	result, err := client.Call("session.branches.list", map[string]string{"session_id": sessionID})
	if err != nil {
		return fmt.Errorf("failed to list branches: %w", err)
	}

	var resp struct {
		Branches []branchInfo `json:"branches"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("failed to parse branches response: %w", err)
	}

	if len(resp.Branches) == 0 {
		fmt.Println("no branches found")
		return nil
	}

	fmt.Println("branches")
	fmt.Println("========")
	fmt.Println()

	for i, b := range resp.Branches {
		summaryTag := ""
		if b.Summary != "" {
			summaryTag = ", summarized"
		}
		currentTag := ""
		if i == 0 {
			currentTag = " (current)"
		}

		if i == 0 {
			fmt.Printf("  %s%s%s\n",
				b.ID,
				currentTag,
				fmt.Sprintf(" (%d msgs%s)", b.MessageCount, summaryTag),
			)
		} else {
			fmt.Printf("  └─ %s%s\n",
				b.ID,
				fmt.Sprintf(" (%d msgs%s)", b.MessageCount, summaryTag),
			)
		}
	}

	return nil
}

type branchInfo = types.BranchInfo

func runBranchSummary(sessionID string) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	if sessionID == "" {
		sessionID, err = resolveMostRecentSessionID(client)
		if err != nil {
			return err
		}
	}

	// Call session.branches.list
	result, err := client.Call("session.branches.list", map[string]string{"session_id": sessionID})
	if err != nil {
		return fmt.Errorf("failed to list branches: %w", err)
	}

	var resp struct {
		Branches []branchInfo `json:"branches"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("failed to parse branches response: %w", err)
	}

	if len(resp.Branches) == 0 {
		fmt.Println("no branches found")
		return nil
	}

	fmt.Println("branch summaries")
	fmt.Println("================")
	fmt.Println()

	for i, b := range resp.Branches {
		currentTag := ""
		if i == 0 {
			currentTag = " (current)"
		}

		if i == 0 {
			fmt.Printf("  %s%s (%d msgs)\n",
				b.ID,
				currentTag,
				b.MessageCount,
			)
		} else {
			fmt.Printf("  └─ %s (%d msgs)\n",
				b.ID,
				b.MessageCount,
			)
		}

		if b.Summary != "" {
			// Indent the summary text under the branch entry
			for _, line := range strings.Split(b.Summary, "\n") {
				fmt.Printf("     %s\n", line)
			}
		} else {
			fmt.Println("     (no summary)")
		}
		fmt.Println()
	}

	return nil
}

func runBranchNavigate(sessionID string, targetMessageID int64) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	if sessionID == "" {
		sessionID, err = resolveMostRecentSessionID(client)
		if err != nil {
			return err
		}
	}

	params := map[string]any{
		"session_id":        sessionID,
		"target_message_id": targetMessageID,
	}

	result, err := client.Call("session.branch.navigate", params)
	if err != nil {
		return fmt.Errorf("failed to navigate branch: %w", err)
	}

	var navResult struct {
		OldLeafID     int64  `json:"old_leaf_id"`
		NewLeafID     int64  `json:"new_leaf_id"`
		NewBranchID   string `json:"new_branch_id"`
		Summary       string `json:"summary,omitempty"`
		AbandonedMsgs int    `json:"abandoned_msgs"`
	}
	if err := json.Unmarshal(result, &navResult); err != nil {
		return fmt.Errorf("failed to parse navigation result: %w", err)
	}

	fmt.Printf("navigated to message %d\n", navResult.NewLeafID)
	fmt.Printf("  old branch leaf: %d\n", navResult.OldLeafID)
	fmt.Printf("  new branch id:   %s\n", navResult.NewBranchID)
	fmt.Printf("  abandoned msgs:  %d\n", navResult.AbandonedMsgs)
	if navResult.Summary != "" {
		summaryPreview := navResult.Summary
		if len(summaryPreview) > 100 {
			summaryPreview = summaryPreview[:97] + "..."
		}
		fmt.Printf("  summary:         %s\n", summaryPreview)
	}

	return nil
}

func runBranchFork(sessionID string, fromMessageID int64, forkName string) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	if sessionID == "" {
		sessionID, err = resolveMostRecentSessionID(client)
		if err != nil {
			return err
		}
	}

	params := map[string]any{
		"session_id":      sessionID,
		"from_message_id": fromMessageID,
		"name":            forkName,
	}

	result, err := client.Call("session.fork", params)
	if err != nil {
		return fmt.Errorf("failed to fork session: %w", err)
	}

	var forkResult struct {
		NewSessionID string `json:"new_session_id"`
		Session      struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"session"`
	}
	if err := json.Unmarshal(result, &forkResult); err != nil {
		return fmt.Errorf("failed to parse fork result: %w", err)
	}

	fmt.Printf("forked session from message %d\n", fromMessageID)
	fmt.Printf("  new session id:  %s\n", forkResult.NewSessionID)
	if forkResult.Session.Name != "" {
		fmt.Printf("  name:            %s\n", forkResult.Session.Name)
	}

	return nil
}

func runBranchTree(sessionID string) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	if sessionID == "" {
		sessionID, err = resolveMostRecentSessionID(client)
		if err != nil {
			return err
		}
	}

	result, err := client.Call("session.tree.get", map[string]string{"session_id": sessionID})
	if err != nil {
		return fmt.Errorf("failed to get tree: %w", err)
	}

	var resp struct {
		Nodes []treeNodeInfo `json:"nodes"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("failed to parse tree response: %w", err)
	}

	if len(resp.Nodes) == 0 {
		fmt.Println("no messages in session")
		return nil
	}

	fmt.Println("conversation tree")
	fmt.Println("=================")
	fmt.Println()

	// Build a simple parent->children map for indentation
	childrenMap := make(map[int64][]treeNodeInfo)
	var roots []treeNodeInfo
	for _, node := range resp.Nodes {
		if node.ParentID == 0 {
			roots = append(roots, node)
		} else {
			childrenMap[node.ParentID] = append(childrenMap[node.ParentID], node)
		}
	}

	var printNode func(node treeNodeInfo, indent string, isLast bool)
	printNode = func(node treeNodeInfo, indent string, isLast bool) {
		prefix := indent
		if isLast {
			prefix += "└─ "
		} else if indent != "" {
			prefix += "├─ "
		}

		leafMarker := ""
		if node.IsLeaf {
			leafMarker = " [leaf]"
		}

		contentPreview := truncateContent(node.Content, 40)

		branchLabel := ""
		if node.BranchID != "" && node.BranchID != "main" {
			branchLabel = fmt.Sprintf(" (%s)", node.BranchID)
		}

		entryTypeLabel := ""
		if node.EntryType != "" && node.EntryType != "message" {
			entryTypeLabel = fmt.Sprintf(" [%s]", node.EntryType)
		}

		fmt.Printf("%s%d %s%s%s%s%s\n",
			prefix, node.ID, node.Role,
			branchLabel, entryTypeLabel, leafMarker,
			contentPreview,
		)

		children := childrenMap[node.ID]
		childIndent := indent
		if indent != "" {
			if isLast {
				childIndent += "   "
			} else {
				childIndent += "│  "
			}
		}
		for i, child := range children {
			printNode(child, childIndent, i == len(children)-1)
		}
	}

	for i, root := range roots {
		printNode(root, "", i == len(roots)-1)
	}

	return nil
}

type treeNodeInfo = types.TreeNodeInfo

func truncateContent(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return ": " + s[:maxLen-3] + "..."
	}
	if s != "" {
		return ": " + s
	}
	return ""
}

// resolveMostRecentSessionID gets the most recent session's ID via the daemon.
func resolveMostRecentSessionID(client daemonClient) (string, error) {
	result, err := client.Call("session.get_most_recent", nil)
	if err != nil {
		return "", fmt.Errorf("failed to get most recent session: %w", err)
	}

	var session struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(result, &session); err != nil {
		return "", fmt.Errorf("failed to parse session response: %w", err)
	}
	if session.ID == "" {
		return "", fmt.Errorf("no active session found")
	}
	return session.ID, nil
}

// daemonClient is the subset of transport.Client used by branch commands.
type daemonClient interface {
	Call(method string, params any) (json.RawMessage, error)
	Close() error
}
