//
//  MCPServer.swift
//  MeeptMenuBar
//
//  Models for the MCP servers management UI (Phase 8 of the MCP default catalog
//  plan). Mirrors the daemon's `ServerStatusEntry` shape (config + stats) and
//  flattens it into a single `MCPServer` row model suitable for SwiftUI Table.
//

import Foundation

// MARK: - Wire types (mirror the daemon JSON)

/// Wrapper for the `GET /api/v1/mcp/servers` response: `{"servers": [...]}`.
struct MCPServersResponse: Codable {
    let servers: [MCPServerStatus]
}

/// Single entry returned by the daemon; pairs a config with its runtime stats.
/// Also the shape returned from `PUT /api/v1/mcp/servers/{name}/enabled`.
struct MCPServerStatus: Codable {
    let config: MCPConfig
    let stats: MCPStats
}

struct MCPConfig: Codable {
    let name: String
    let enabled: Bool?
    let description: String?
    let category: String?
}

struct MCPStats: Codable {
    let state: String
    let requests: Int
    let errors: Int
    let lastError: String?
    // ISO-8601 timestamps; optional because the daemon omits them when never
    // set. Kept on the wire type for forward use; not surfaced in the row model.
    let lastErrorAt: String?
    let lastRequestAt: String?
}

// MARK: - Row model used by the UI

/// Flattened, Identifiable row suitable for SwiftUI `Table` / `ForEach`.
/// Built from `MCPServerStatus`; never decoded directly from JSON.
struct MCPServer: Codable, Identifiable {
    let id: String          // == config.name
    let name: String
    var enabled: Bool       // var for optimistic toggling
    let category: String?
    let description: String?
    let state: String       // "active" | "inactive" | "error" | "disabled"
    let requests: Int
    let errors: Int
    let lastError: String?
}

extension MCPServer {
    /// Initialize a row from a daemon `ServerStatusEntry`.
    init(from status: MCPServerStatus) {
        let cfg = status.config
        let st = status.stats
        self.id = cfg.name
        self.name = cfg.name
        // nil enabled means "treated as true" per the spec (backward compat
        // for configs that predate the enabled field).
        self.enabled = cfg.enabled ?? true
        self.category = cfg.category
        self.description = cfg.description
        self.state = st.state
        self.requests = st.requests
        self.errors = st.errors
        self.lastError = st.lastError
    }

    /// Returns a copy with the `enabled` flag replaced. Used by the view model
    /// for optimistic updates before the PUT round-trip completes.
    func withEnabled(_ newValue: Bool) -> MCPServer {
        MCPServer(
            id: id,
            name: name,
            enabled: newValue,
            category: category,
            description: description,
            state: state,
            requests: requests,
            errors: errors,
            lastError: lastError
        )
    }
}
