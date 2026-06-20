//
//  MCPViewModel.swift
//  MeeptMenuBar
//
//  View model for the MCP servers "tools" tab in Settings.
//  Polls `GET /api/v1/mcp/servers` every 5s while visible and supports
//  optimistic enable/disable via `PUT /api/v1/mcp/servers/{name}/enabled`.
//

import Combine
import SwiftUI
import os.log

@MainActor
class MCPViewModel: ObservableObject {
    @Published var servers: [MCPServer] = []
    @Published var isLoading = false
    @Published var errorMessage: String?
    @Published var showError = false

    private let api: APIClient
    private var pollTimer: Timer?
    private let updateInterval: TimeInterval = 5.0
    /// Names with an in-flight PUT. Used to disable the toggle in the UI so
    /// the user cannot double-click. MainActor-only so no lock needed.
    private var pendingToggles: Set<String> = []
    private let logger = Logger(subsystem: "com.caimlas.meept.menubar", category: "MCPViewModel")

    init(api: APIClient) {
        self.api = api
    }

    // MARK: - Polling

    func startPolling() {
        refresh()
        pollTimer?.invalidate()
        // Timer(timeInterval:...) + RunLoop.common so the timer keeps firing
        // during modal tracking (menus, scroll). Same pattern as
        // DaemonStatusViewModel and MetricsViewModel.
        let timer = Timer(timeInterval: updateInterval, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.refresh()
            }
        }
        RunLoop.main.add(timer, forMode: .common)
        pollTimer = timer
    }

    func stopPolling() {
        pollTimer?.invalidate()
        pollTimer = nil
    }

    // MARK: - Fetch

    func refresh() {
        // Guard against overlapping refreshes; polling timers may fire while a
        // prior fetch is still in flight. We do not set `isLoading` for
        // background polling to avoid UI flicker.
        guard !isLoading else { return }
        isLoading = true
        Task { [weak self] in
            guard let self else { return }
            do {
                let result = try await api.getMCPServers()
                servers = result
                errorMessage = nil
            } catch {
                // Don't surface an alert for routine polling failures — would
                // be too noisy if the daemon is briefly down. The error is
                // logged for diagnostics.
                logger.error("failed to fetch mcp servers: \(error.localizedDescription)")
                errorMessage = error.localizedDescription
            }
            isLoading = false
        }
    }

    // MARK: - Toggle

    func toggleEnabled(_ server: MCPServer) async {
        let targetName = server.name
        let newState = !server.enabled
        // Optimistic update: flip the row immediately so the toggle feels
        // responsive, then revert on error via the next refresh.
        if let idx = servers.firstIndex(where: { $0.id == server.id }) {
            servers[idx] = servers[idx].withEnabled(newState)
        }
        pendingToggles.insert(targetName)
        defer { pendingToggles.remove(targetName) }
        do {
            let updated = try await api.setMCPEnabled(name: targetName, enabled: newState)
            if let idx = servers.firstIndex(where: { $0.id == updated.id }) {
                servers[idx] = updated
            }
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
            showError = true
            // Revert local state by re-fetching the canonical list.
            refresh()
        }
    }

    func isPending(_ server: MCPServer) -> Bool {
        pendingToggles.contains(server.name)
    }
}
