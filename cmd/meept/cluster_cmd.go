// Package main implements the cluster CLI commands.
package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	c25519 "golang.org/x/crypto/curve25519"
)

const stateDirCluster = "cluster"

// ---------------------------------------------------------------------------
// Root command
// ---------------------------------------------------------------------------

func newClusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage meept cluster",
		Long:  `Commands for initializing, joining, and managing meept clusters.`,
	}

	cmd.AddCommand(newClusterInitCmd())
	cmd.AddCommand(newClusterJoinCmd())
	cmd.AddCommand(newClusterStartCmd())
	cmd.AddCommand(newClusterStatusCmd())
	cmd.AddCommand(newClusterLeaveCmd())
	cmd.AddCommand(newClusterKeygenCmd())
	cmd.AddCommand(newClusterRemoteCmd())
	cmd.AddCommand(newClusterDebugCmd())

	return cmd
}


// ---------------------------------------------------------------------------
// cluster init
// ---------------------------------------------------------------------------

func newClusterInitCmd() *cobra.Command {
	var (
		clusterName string
		nodeName    string
		nodeID      string
		gitRemote   string
		force       bool
		configFlag  string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new cluster (interactive wizard)",
		Long: `Initialize a new meept cluster by creating configuration,
generating encryption keys (ed25519 for node signatures), and registering
the node in a git-based node registry.

An interactive wizard is presented unless --cluster-name is provided.

Examples:
  meept cluster init
  meept cluster init --cluster-name "production" --node-name "node-1" --git-remote https://git.example.com/cluster.git`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nonInteractive := cmd.Flags().Changed("yes")
			// Also detect non-interactive when stdin is not a TTY
			if fi, err := os.Stdin.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) == 0 {
				nonInteractive = true
			}
			return runClusterInit(cmd, clusterName, nodeName, nodeID, gitRemote, force, configFlag, nonInteractive)
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster-name", "", "cluster name (skip name prompt)")
	cmd.Flags().StringVar(&nodeName, "node-name", "", "node display name (skip name prompt)")
	cmd.Flags().StringVar(&nodeID, "node-id", "", "node ID (skip ID prompt)")
	cmd.Flags().StringVar(&gitRemote, "git-remote", "", "git remote URL for cluster registry (skip remote prompt)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing cluster config")
	cmd.Flags().Bool("yes", false, "skip interactive prompts")
	cmd.Flags().StringVar(&configFlag, "config", "", "path to output config file (default: $state_dir/cluster/config.json5)")

	return cmd
}

// runClusterInit presents an interactive wizard and writes cluster config.
func runClusterInit(cmd *cobra.Command, clusterName, nodeName, nodeID, gitRemote string, force bool, configPath string, nonInteractive bool) error {
	// Determine config path
	if configPath == "" {
		configPath = filepath.Join(stateDir, stateDirCluster, "config.json5")
	}
	configPath = filepath.Clean(configPath)

	// Check if config already exists
	if !force {
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("cluster config already exists at %s; use --force to overwrite", configPath)
		}
	}

	// --- node ID --------------------------------------------------------
	nodeID = promptLine(nodeID, "Node ID (leave empty for auto-generated):")
	if nodeID == "" {
		var err error
		nodeID, err = generateNodeID()
		if err != nil {
			return fmt.Errorf("failed to generate node ID: %w", err)
		}
	}
	nodeID = sanitizeNodeID(nodeID)

	// --- key generation --------------------------------------------------
	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate ed25519 key: %w", err)
	}

	wgKey, err := generateWireGuardKey()
	if err != nil {
		return fmt.Errorf("failed to generate WireGuard key: %w", err)
	}

	// --- cluster name ----------------------------------------------------
	clusterName = promptLine(clusterName, "Cluster name:")
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" {
		clusterName = "meept-cluster"
	}

	// --- node display name -----------------------------------------------
	nodeName = promptLine(nodeName, "Node display name:")
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		nodeName = nodeID
	}

	// --- git remote ------------------------------------------------------
	gitRemote = promptLine(gitRemote, "Git remote URL for cluster registry (optional, leave empty to skip):")
	// Validate URL if provided
	if gitRemote != "" {
		if _, err := url.Parse(gitRemote); err != nil {
			return fmt.Errorf("invalid git remote URL %q: %w", gitRemote, err)
		}
	}

	// --- join key --------------------------------------------------------
	joinKey, err := generateJoinKey()
	if err != nil {
		return fmt.Errorf("failed to generate join key: %w", err)
	}

	// --- build config ----------------------------------------------------
	clusterID, err := generateClusterID()
	if err != nil {
		return fmt.Errorf("failed to generate cluster ID: %w", err)
	}

	cfg := &clusterConfigJSON{
		ClusterID:       clusterID,
		ClusterName:     clusterName,
		NodeID:          nodeID,
		NodeName:        nodeName,
		SigningPub:      hex.EncodeToString(edPub),
		SigningPriv:     hex.EncodeToString(edPriv),
		WireGuardPub:    wgKey.Public().String(),
		WireGuardPriv:   wgKey.Private().String(),
		NetworkSubnet:   "10.200.0.0/24",
		NetworkPort:     51820,
		NetworkInterface: "wg0",
		ClusterIP:       "10.200.0.1",
		GossipHeartbeat:   "30s",
		GossipPeerTimeout: "2m",
		GossipEventRetention: "1h",
		GossipMaxRetry:    3,
		QueueClaimTimeout:     "5m",
		QueueReachTimeout:     "2m",
		QueueFullPayload:      true,
		GitRemote:        gitRemote,
		GitSyncInterval:  "5m",
		GitHeartbeat:     false,
		GitBranch:        "cluster",
		SecurityRequireSigs: true,
		SecurityKeyRotateDays: 90,
		JoinKey:          joinKey,
		Status:           "initialized",
	}

	// --- write files -----------------------------------------------------
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Write signing private key to a separate protected file
	privPath := filepath.Join(dir, "node_private_key")
	if err := os.WriteFile(privPath, []byte(hex.EncodeToString(edPriv)+"\n"), 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	// --- output ----------------------------------------------------------
	fmt.Println("===========================================")
	fmt.Println("     cluster initialized successfully")
	fmt.Println("===========================================")
	fmt.Println()
	fmt.Printf("  Cluster:    %s\n", cfg.ClusterName)
	fmt.Printf("  Cluster ID: %s\n", cfg.ClusterID)
	fmt.Printf("  Node ID:    %s\n", cfg.NodeID)
	fmt.Printf("  Config:     %s\n", configPath)
	fmt.Printf("  Private Key: %s\n", privPath)
	fmt.Println()
	fmt.Println("  important: keep the private key file secure!")
	fmt.Println()

	// Attempt git remote setup if URL provided and git is available
	if gitRemote != "" {
		if err := addGitRemote(gitRemote); err != nil {
			fmt.Fprintf(os.Stderr, "warning: git remote setup failed: %v\n", err)
		}
	}

	fmt.Println("next steps:")
	fmt.Println("  1. Other nodes can join with: meept cluster join <join-key>")
	if gitRemote != "" {
		fmt.Println("  2. Share the cluster registry via git")
	}
	fmt.Println("  3. Start each node with: meept cluster start")

	return nil
}

// ---------------------------------------------------------------------------
// cluster join
// ---------------------------------------------------------------------------

func newClusterJoinCmd() *cobra.Command {
	var configFlag string

	cmd := &cobra.Command{
		Use:   "join <join-key>",
		Short: "Join an existing cluster with a join key",
		Long: `Join an existing meept cluster using a join key.
The join key is used to authenticate with the cluster and receive
configuration from the coordinating node.

The join key can be passed as an argument or via stdin:
  meept cluster join <join-key>
  echo <join-key> | meept cluster join`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var joinKey string
			if len(args) > 0 {
				joinKey = args[0]
			} else {
				joinKey = readStdin()
			}
			if joinKey == "" {
				// Interactive fallback
				joinKey = promptLine("", "Join key:")
			}
			if joinKey == "" {
				return fmt.Errorf("no join key provided")
			}

			if configFlag == "" {
				configFlag = filepath.Join(stateDir, stateDirCluster, "config.json5")
			}
			return runClusterJoin(joinKey, filepath.Clean(configFlag))
		},
	}

	cmd.Flags().StringVar(&configFlag, "config", "", "path to output config file (default: $state_dir/cluster/config.json5)")

	return cmd
}

func runClusterJoin(joinKey, configPath string) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer client.Close()

	raw, err := client.Call("cluster.join", map[string]any{
		"join_key": joinKey,
	})
	if err != nil {
		return fmt.Errorf("cluster join failed: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Printf("Cluster:    %s\n", getStringOr(result, "cluster_name", "unknown"))
	fmt.Printf("Node ID:    %s\n", getStringOr(result, "node_id", "unknown"))
	fmt.Printf("Config:     %s\n", configPath)

	// Write config if returned
	if cfgData, ok := result["config"].(json.RawMessage); ok && len(cfgData) > 0 {
		dir := filepath.Dir(configPath)
		if err := os.MkdirAll(dir, 0700); err == nil {
			_ = os.WriteFile(configPath, cfgData, 0600)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// cluster start
// ---------------------------------------------------------------------------

func newClusterStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start cluster coordination",
		Long: `Start the cluster coordination engine.
This initializes the gossip protocol, message bus forwarding, and
task queue replication across the cluster mesh.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			raw, err := client.Call("cluster.start", map[string]any{})
			if err != nil {
				return fmt.Errorf("cluster start failed: %w", err)
			}

			var result map[string]any
			if err := json.Unmarshal(raw, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			status := getStringOr(result, "status", "started")
			fmt.Printf("Cluster: %s\n", status)

			if nodeID, ok := result["node_id"].(string); ok && nodeID != "" {
				fmt.Printf("Node ID: %s\n", nodeID)
			}
			if cluster, ok := result["cluster_name"].(string); ok && cluster != "" {
				fmt.Printf("Cluster: %s\n", cluster)
			}

			return nil
		},
	}

	return cmd
}

// ---------------------------------------------------------------------------
// cluster status
// ---------------------------------------------------------------------------

func newClusterStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show cluster membership and task state",
		Long:  `Display the current cluster membership, node status, and task queue state.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			raw, err := client.Call("cluster.status", map[string]any{})
			if err != nil {
				return fmt.Errorf("cluster status failed: %w", err)
			}

			if jsonOutput {
				output, err := json.MarshalIndent(raw, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(output))
				return nil
			}

			var result map[string]any
			if err := json.Unmarshal(raw, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			membersRaw, ok := result["members"]
			if !ok || membersRaw == nil {
				fmt.Println("  members: (none)")
				return nil
			}
			members, ok := membersRaw.([]any)
			if !ok {
				fmt.Println("  members: (unexpected format)")
				return nil
			}
			nodeID := getStringOr(result, "node_id", "unknown")

			fmt.Printf("Cluster Status\n")
			fmt.Printf("=============\n\n")
			fmt.Printf("  Node ID:    %s\n", nodeID)
			fmt.Printf("  Cluster:    %s\n", getStringOr(result, "cluster_name", "none"))
			fmt.Printf("  Status:     %s\n", getStringOr(result, "status", "not started"))

			if len(members) > 0 {
				fmt.Printf("\nMembers (%d):\n", len(members))
				fmt.Printf("  %-35s %-10s %-16s %-14s\n", "NODE", "STATUS", "IP", "HEARTBEAT")
				fmt.Printf("  %-35s %-10s %-16s %-14s\n",
					strings.Repeat("-", 35), strings.Repeat("-", 10),
					strings.Repeat("-", 16), strings.Repeat("-", 14))

				for _, m := range members {
					mem, ok := m.(map[string]any)
					if !ok {
						continue
					}
					id := getStringOr(mem, "node_id", "?")
					st := getStringOr(mem, "status", "?")
					ip := getStringOr(mem, "cluster_ip", "-")
					hb := getStringOr(mem, "last_heartbeat", "-")

					if len(id) > 35 {
						id = id[:32] + "..."
					}

					fmt.Printf("  %-35s %-10s %-16s %-14s\n", id, st, ip, hb)
				}
			}

			// Task stats
			if tasks, ok := result["tasks"].(map[string]any); ok {
				fmt.Printf("\nTasks\n")
				fmt.Printf("-----\n")
				for state, count := range tasks {
					fmt.Printf("  %-12s %v\n", state+":", count)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

// ---------------------------------------------------------------------------
// cluster leave
// ---------------------------------------------------------------------------

func newClusterLeaveCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "leave",
		Short: "Gracefully leave the cluster",
		Long:  `Gracefully leave the cluster, notifying other nodes and transferring any owned tasks.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			raw, err := client.Call("cluster.leave", map[string]any{
				"force": force,
			})
			if err != nil {
				return fmt.Errorf("cluster leave failed: %w", err)
			}

			var result map[string]any
			if err := json.Unmarshal(raw, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			fmt.Printf("Left cluster: %s\n", getStringOr(result, "message", "done"))
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force leave without graceful shutdown")

	return cmd
}

// ---------------------------------------------------------------------------
// cluster keygen
// ---------------------------------------------------------------------------

func newClusterKeygenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keygen",
		Short: "Generate encryption keys for a new cluster node",
		Long: `Generate a new ed25519 signing key pair and WireGuard key pair.
Outputs public keys suitable for sharing with cluster members.

Example:
  meept cluster keygen                  # generate and display keys
  meept cluster keygen --output dir     # write keys to directory`,
		RunE: func(cmd *cobra.Command, args []string) error {
			edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				return fmt.Errorf("failed to generate ed25519 key: %w", err)
			}

			wgKey, err := generateWireGuardKey()
			if err != nil {
				return fmt.Errorf("failed to generate WireGuard key: %w", err)
			}

			fmt.Println("ed25519 signing key:")
			fmt.Printf("  public:  %s\n", hex.EncodeToString(edPub))
			fmt.Printf("  private: %s\n", hex.EncodeToString(edPriv))
			fmt.Println()
			fmt.Println("wireguard mesh key:")
			fmt.Printf("  public:  %s\n", wgKey.Public().String())
			fmt.Printf("  private: %s\n", wgKey.Private().String())
			return nil
		},
	}

	return cmd
}

// ---------------------------------------------------------------------------
// cluster debug
// ---------------------------------------------------------------------------

func newClusterDebugCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Show cluster debug information",
		Long:  `Display cluster internals for debugging: event log and peer connectivity.`,
	}

	cmd.AddCommand(newClusterDebugEventsCmd())
	cmd.AddCommand(newClusterDebugPeersCmd())

	return cmd
}

func newClusterDebugEventsCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "events",
		Short: "show recent cluster events",
		Long:  `Display recent cluster events from the event log. Events are shown in reverse chronological order.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			if limit <= 0 {
				limit = 50
			}

			raw, err := client.Call("cluster.debug.events", map[string]any{
				"limit": limit,
			})
			if err != nil {
				return fmt.Errorf("cluster debug events failed: %w", err)
			}

			var events []map[string]any
			if err := json.Unmarshal(raw, &events); err != nil {
				return fmt.Errorf("failed to parse events: %w", err)
			}

			if len(events) == 0 {
				fmt.Println("(no cluster events)")
				return nil
			}

			fmt.Printf("%-36s %-16s %-20s %-30s %s\n",
				"EVENT_ID", "TYPE", "NODE", "TIMESTAMP", "PAYLOAD_SUMMARY")
			fmt.Printf("%s %s %s %s %s\n",
				strings.Repeat("-", 36), strings.Repeat("-", 16),
				strings.Repeat("-", 20), strings.Repeat("-", 30),
				strings.Repeat("-", 40))

			for _, ev := range events {
				eventID := getStringOr(ev, "event_id", "?")
				evType := getStringOr(ev, "event_type", "?")
				nodeID := getStringOr(ev, "node_id", "?")
				ts := getStringOr(ev, "timestamp", "?")
				payload := getStringOr(ev, "payload_summary", "")

				if len(eventID) > 36 {
					eventID = eventID[:33] + "..."
				}
				if len(nodeID) > 20 {
					nodeID = nodeID[:17] + "..."
				}
				if len(ts) > 30 {
					ts = ts[:27] + "..."
				}
				if len(payload) > 40 {
					payload = payload[:37] + "..."
				}

				fmt.Printf("%-36s %-16s %-20s %-30s %s\n",
					eventID, evType, nodeID, ts, payload)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 50, "maximum number of events to show (1-1000)")

	return cmd
}

func newClusterDebugPeersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "peers",
		Short: "show peer connectivity",
		Long:  `Display all known cluster peers and their connectivity status.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			raw, err := client.Call("cluster.peers", map[string]any{})
			if err != nil {
				return fmt.Errorf("cluster peers failed: %w", err)
			}

			var peers []map[string]any
			if err := json.Unmarshal(raw, &peers); err != nil {
				return fmt.Errorf("failed to parse peers: %w", err)
			}

			if len(peers) == 0 {
				fmt.Println("(no peers connected)")
				return nil
			}

			fmt.Printf("%-36s %-30s %-10s %-20s\n",
				"NODE_ID", "ENDPOINT", "STATUS", "LAST_SEEN")
			fmt.Printf("%s %s %s %s\n",
				strings.Repeat("-", 36), strings.Repeat("-", 30),
				strings.Repeat("-", 10), strings.Repeat("-", 20))

			for _, p := range peers {
				nodeID := getStringOr(p, "node_id", getStringOr(p, "NodeID", "?"))
				endpoint := getStringOr(p, "endpoint", getStringOr(p, "Endpoint", "-"))
				status := getStringOr(p, "status", getStringOr(p, "Status", "?"))
				lastSeen := getStringOr(p, "last_seen", getStringOr(p, "LastSeen", "-"))
				joinedAt := getStringOr(p, "joined_at", getStringOr(p, "JoinedAt", ""))

				if len(nodeID) > 36 {
					nodeID = nodeID[:33] + "..."
				}
				if len(endpoint) > 30 {
					endpoint = endpoint[:27] + "..."
				}

				// If last_seen is empty, use joined_at as fallback
				if lastSeen == "-" && joinedAt != "" {
					lastSeen = joinedAt
				}

				fmt.Printf("%-36s %-30s %-10s %-20s\n",
					nodeID, endpoint, status, lastSeen)
			}

			return nil
		},
	}

	return cmd
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// remote command group
// ---------------------------------------------------------------------------

func newClusterRemoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage git remotes for cluster registry",
		Long: `Manage git remotes used for synchronizing cluster membership.`,
	}

	cmd.AddCommand(newClusterRemoteAddCmd())
	cmd.AddCommand(newClusterRemoteRemoveCmd())
	cmd.AddCommand(newClusterRemoteListCmd())

	return cmd
}

func newClusterRemoteAddCmd() *cobra.Command {
	var remoteName string

	cmd := &cobra.Command{
		Use:   "add <url>",
		Short: "Add a git remote for cluster registry synchronization",
		Long: `Add a git remote to the cluster for distributing its registry
(supporting nodes and their public keys).

The remote is configured as a local git remote and used for
pulling/syncing cluster membership metadata.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registryPath := filepath.Join(stateDir, stateDirCluster, "registry")
			remoteURL := args[0]
			if remoteName == "" {
				remoteName = "cluster"
			}

			// Initialize local registry if needed
			if err := os.MkdirAll(registryPath, 0755); err != nil {
				return fmt.Errorf("failed to create registry: %w", err)
			}

			// Try to init git repo
			initCmd := exec.Command("git", "-C", registryPath, "init", "-b", "cluster")
			_ = initCmd.Run()

			// Add remote
			addCmd := exec.Command("git", "-C", registryPath, "remote", "add", remoteName, remoteURL)
			output, err := addCmd.CombinedOutput()
			if err != nil {
				// remote might already exist
				if !strings.Contains(string(output), "already exists") {
					return fmt.Errorf("failed to add git remote: %w\n%s", err, string(output))
				}
				// Update if already exists
				updateCmd := exec.Command("git", "-C", registryPath, "remote", "set-url", remoteName, remoteURL)
				if out, err := updateCmd.CombinedOutput(); err != nil {
					return fmt.Errorf("failed to update git remote: %w\n%s", err, string(out))
				}
				fmt.Printf("Updated git remote %q: %s\n", remoteName, remoteURL)
			} else {
				fmt.Printf("Added git remote %q: %s\n", remoteName, remoteURL)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&remoteName, "name", "", "remote name (default: \"cluster\")")

	return cmd
}

func newClusterRemoteRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a git remote for cluster registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registryPath := filepath.Join(stateDir, stateDirCluster, "registry")
			_, err := exec.Command("git", "-C", registryPath, "remote", "remove", args[0]).CombinedOutput()
			if err != nil {
				return fmt.Errorf("failed to remove git remote: %w", err)
			}
			fmt.Printf("Removed git remote: %s\n", args[0])
			return nil
		},
	}
}

func newClusterRemoteListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured git remotes for cluster registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			registryPath := filepath.Join(stateDir, stateDirCluster, "registry")
			out, err := exec.Command("git", "-C", registryPath, "remote", "-v").CombinedOutput()
			if err != nil {
				fmt.Println("(no remotes configured)")
				return nil
			}
			fmt.Print(string(out))
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// promptLine prompts the user for a line of input, using the default if non-empty.
func promptLine(defaultVal, prompt string) string {
	input := reader()
	fmt.Print(prompt)
	if defaultVal != "" {
		fmt.Printf(" [%s] ", defaultVal)
	}
	line, _ := input.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

// promptInput is an alias for promptLine for backward compatibility.
// The reader parameter is unused but retained for API compatibility.
func promptInput(_ *bufio.Reader, prompt, defaultVal string) string {
	return promptLine(defaultVal, prompt)
}

// reader returns a buffered reader for stdin.
func reader() *bufio.Reader {
	return bufio.NewReader(os.Stdin)
}

// readStdin reads all of stdin and returns it as a trimmed string.
func readStdin() string {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// generateNodeID creates a short random node identifier.
func generateNodeID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generateNodeID: %w", err)
	}
	return fmt.Sprintf("node-%x", b), nil
}

// generateClusterID creates a unique cluster identifier.
func generateClusterID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generateClusterID: %w", err)
	}
	return fmt.Sprintf("cluster-%x", b), nil
}

// generateJoinKey creates a random join key.
func generateJoinKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generateJoinKey: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// sanitizeNodeID removes invalid characters from a node ID.
func sanitizeNodeID(id string) string {
	var sb strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteByte(byte(r))
		}
	}
	return sb.String()
}

// generateWireGuardKey generates a WireGuard-compatible ed25519 key pair.
func generateWireGuardKey() (*wireGuardKey, error) {
	// WireGuard uses its own curve25519 implementation.
	// We use the refactored curve25519 package from golang.org/x/crypto.
	// For now, generate raw 32-byte keys compatible with WireGuard.
	priv := make([]byte, 32)
	if _, err := rand.Read(priv); err != nil {
		return nil, err
	}
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	pub, err := x25519(priv)
	if err != nil {
		return nil, err
	}

	return &wireGuardKey{
		priv: priv,
		pub:  pub,
	}, nil
}

// wireGuardKey holds a WireCurve25519 key pair.
type wireGuardKey struct {
	priv []byte
	pub  []byte
}

// Public returns the base64-encoded public key.
func (k *wireGuardKey) Public() wireGuardPublicKey {
	return wireGuardPublicKey{pub: k.pub}
}

// Private returns the base64-encoded private key.
func (k *wireGuardKey) Private() wireGuardPrivateKey {
	return wireGuardPrivateKey{priv: k.priv}
}

// wireGuardPublicKey is a WireGuard public key.
type wireGuardPublicKey struct {
	pub []byte
}

func (k wireGuardPublicKey) String() string {
	return base64.StdEncoding.EncodeToString(k.pub)
}

// wireGuardPrivateKey is a WireGuard private key.
type wireGuardPrivateKey struct {
	priv []byte
}

func (k wireGuardPrivateKey) String() string {
	return base64.StdEncoding.EncodeToString(k.priv)
}

// curve25519 generates a Curve25519 public key from a private key.
func x25519(privKey []byte) ([]byte, error) {
	if len(privKey) != 32 {
		return nil, fmt.Errorf("x25519: bad private key length: %d", len(privKey))
	}
	pub, err := c25519.X25519(privKey, c25519.Basepoint)
	if err != nil {
		return nil, err
	}
	return pub, nil
}

// ---------------------------------------------------------------------------
// Git helper
// ---------------------------------------------------------------------------

// addGitRemote configures a git remote in the cluster registry directory.
// Returns an error if git operations fail (other than "already exists" cases).
func addGitRemote(remoteURL string) error {
	clusterRepoPath := filepath.Join(stateDir, stateDirCluster, "registry")
	if err := os.MkdirAll(clusterRepoPath, 0755); err != nil {
		return fmt.Errorf("failed to create registry directory: %w", err)
	}

	// Try git init (ok if already initialized)
	initCmd := exec.Command("git", "-C", clusterRepoPath, "init", "-b", "cluster")
	if output, err := initCmd.CombinedOutput(); err != nil {
		if !strings.Contains(string(output), "already exists") {
			return fmt.Errorf("git init failed: %w\n%s", err, string(output))
		}
	}

	// Add remote
	addCmd := exec.Command("git", "-C", clusterRepoPath, "remote", "add", "cluster", remoteURL)
	if output, err := addCmd.CombinedOutput(); err != nil {
		if !strings.Contains(string(output), "already exists") {
			return fmt.Errorf("failed to add git remote: %w\n%s", err, string(output))
		}
		// Update if already exists
		updateCmd := exec.Command("git", "-C", clusterRepoPath, "remote", "set-url", "cluster", remoteURL)
		if out, err := updateCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to update git remote: %w\n%s", err, string(out))
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Config structures for JSON marshaling
// ---------------------------------------------------------------------------

// clusterConfigJSON is the JSON representation stored in the cluster config file.
type clusterConfigJSON struct {
	ClusterID       string `json:"cluster_id"`
	ClusterName     string `json:"cluster_name"`
	NodeID          string `json:"node_id"`
	NodeName        string `json:"node_name"`
	SigningPub      string `json:"signing_pubkey"`
	SigningPriv     string `json:"signing_private_key"`
	WireGuardPub    string `json:"wireguard_pubkey"`
	WireGuardPriv   string `json:"wireguard_private_key"`
	NetworkSubnet   string `json:"wireguard_subnet"`
	NetworkPort     int    `json:"wireguard_port"`
	NetworkInterface string `json:"interface"`
	ClusterIP       string `json:"cluster_ip"`
	GossipHeartbeat   string `json:"heartbeat_interval"`
	GossipPeerTimeout string `json:"peer_timeout"`
	GossipEventRetention string `json:"event_retention"`
	GossipMaxRetry    int    `json:"max_retry_attempts"`
	QueueClaimTimeout     string `json:"default_claim_timeout"`
	QueueReachTimeout     string `json:"node_reachability_timeout"`
	QueueFullPayload      bool   `json:"full_payload_replication"`
	GitRemote        string `json:"git_remote"`
	GitSyncInterval  string `json:"sync_interval"`
	GitHeartbeat     bool   `json:"heartbeat_commit"`
	GitBranch        string `json:"branch"`
	SecurityRequireSigs bool `json:"require_node_signatures"`
	SecurityKeyRotateDays int `json:"key_rotation_days"`
	JoinKey          string `json:"join_key"`
	Status           string `json:"status"`
}

// clusterConfig is the in-memory config loaded from the config file.
// For future daemon-side use.
type clusterConfig struct {
	ClusterID       string          `json:"cluster_id"`
	ClusterName     string          `json:"cluster_name"`
	NodeID          string          `json:"node_id"`
	NodeName        string          `json:"node_name"`
	SigningPub      string          `json:"signing_pubkey"`
	SigningPriv     string          `json:"signing_private_key"`
	WireGuardPub    string          `json:"wireguard_pubkey"`
	WireGuardPriv   string          `json:"wireguard_private_key"`
	Network         networkConfig   `json:"network"`
	ClusterIP       string          `json:"cluster_ip"`
	Gossip          gossipConfig    `json:"gossip"`
	Queue           queueConfig     `json:"queue"`
	Git             gitConfig       `json:"git"`
	Security        securityConfig  `json:"security"`
	JoinKey         string          `json:"join_key"`
	Status          string          `json:"status"`
}

type networkConfig struct {
	Subnet   string `json:"wireguard_subnet"`
	Port     int    `json:"wireguard_port"`
	Interface string `json:"interface"`
}

type gossipConfig struct {
	HeartbeatInterval string `json:"heartbeat_interval"`
	PeerTimeout       string `json:"peer_timeout"`
	EventRetention    string `json:"event_retention"`
	MaxRetryAttempts  int    `json:"max_retry_attempts"`
}

type queueConfig struct {
	DefaultClaimTimeout     string `json:"default_claim_timeout"`
	NodeReachabilityTimeout string `json:"node_reachability_timeout"`
	FullPayloadReplication  bool   `json:"full_payload_replication"`
}

type gitConfig struct {
	Remote        string `json:"remote"`
	SyncInterval  string `json:"sync_interval"`
	HeartbeatCommit bool `json:"heartbeat_commit"`
	Branch        string `json:"branch"`
}

type securityConfig struct {
	RequireNodeSignatures bool `json:"require_node_signatures"`
	KeyRotationDays       int  `json:"key_rotation_days"`
}
