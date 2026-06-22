//
//  SessionBadgeManager.swift
//  MeeptMenuBar
//

import Foundation
import AppKit
import Combine
import os.log

@MainActor
class SessionBadgeManager: ObservableObject {
    @Published var designatedCount: Int = 0
    @Published var hasUrgentSessions: Bool = false
    @Published var hasHighPrioritySessions: Bool = false
    @Published var urgentCount: Int = 0
    @Published var highCount: Int = 0

    private let apiClient: APIClient
    private var badgeTimer: Timer?
    private let logger = Logger(
        subsystem: "com.caimlas.meept.menubar",
        category: "SessionBadgeManager"
    )

    init(apiClient: APIClient) {
        self.apiClient = apiClient
    }

    // MARK: - Polling

    func startPolling(interval: TimeInterval = 5.0) {
        badgeTimer?.invalidate()
        let timer = Timer(timeInterval: interval, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.fetchDesignatedSessions()
            }
        }
        RunLoop.main.add(timer, forMode: .common)
        badgeTimer = timer
        fetchDesignatedSessions()
    }

    func stopPolling() {
        badgeTimer?.invalidate()
        badgeTimer = nil
    }

    // MARK: - Fetch

    func fetchDesignatedSessions() {
        Task { [weak self] in
            guard let self else { return }
            do {
                let response = try await apiClient.getDesignatedSessions()

                // Snapshot new counts before dispatching to main queue
                var newUrgentCount = 0
                var newHighCount = 0
                var newHasUrgent = false
                var newHasHighOrMore = false
                for summary in response.sessions {
                    switch summary.designation.priority {
                    case .urgent:
                        newUrgentCount += 1
                    case .high:
                        newHighCount += 1
                    default:
                        break
                    }
                }
                newHasUrgent = newUrgentCount > 0
                newHasHighOrMore = newHighCount > 0

                DispatchQueue.main.async {
                    let oldHasUrgent = self.hasUrgentSessions
                    let oldHasHigh = self.hasHighPrioritySessions
                    let oldCount = self.designatedCount

                    self.designatedCount = response.designatedCount
                    self.urgentCount = newUrgentCount
                    self.highCount = newHighCount
                    self.hasUrgentSessions = newHasUrgent
                    self.hasHighPrioritySessions = newHasHighOrMore

                    // Show a native notification when designated sessions
                    // change (new sessions appear, or an existing one moves
                    // to a more urgent state).
                    let wasSilent = oldCount == 0 && self.designatedCount > 0
                    let escalatedToUrgent = !oldHasUrgent && self.hasUrgentSessions
                    let escalatedToHigh = !oldHasHigh && self.hasHighPrioritySessions

                    if wasSilent || escalatedToUrgent || escalatedToHigh {
                        let title = "Meept — \(self.designatedCount) session\(self.designatedCount == 1 ? "" : "s") waiting"
                        var body = "\(self.urgentCount) urgent, \(self.highCount) high-priority"
                        // Include the most urgent session name
                        if let busiest = response.sessions.min(by: { $0.designation.priority.orderValue > $1.designation.priority.orderValue }) {
                            body += ": \(busiest.name)"
                        }
                        let bestPriority = response.sessions
                            .map { $0.designation.priority }
                            .max() ?? .normal
                        NotificationManager.shared.showSessionNotification(
                            title: title,
                            message: body,
                            status: .waitingHuman,
                            priority: bestPriority
                        )
                    }
                }
            } catch {
                self.logger.error(
                    "failed to fetch designated sessions: \(error.localizedDescription)"
                )
            }
        }
    }

    func acknowledgeSession(_ sessionID: String) {
        Task { [weak self] in
            guard let self else { return }
            do {
                try await apiClient.acknowledgeSession(sessionID)
            } catch {
                self.logger.error(
                    "failed to acknowledge session \(sessionID): \(error.localizedDescription)"
                )
            }
        }
    }

    // MARK: - Badge Display

    var badgeSymbol: String? {
        if hasUrgentSessions { return "exclamationmark.circle.fill" }
        if hasHighPrioritySessions { return "circle.fill" }
        if designatedCount > 0 { return "bell.fill" }
        return nil
    }
}
