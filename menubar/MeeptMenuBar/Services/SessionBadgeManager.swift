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
                DispatchQueue.main.async {
                    self.designatedCount = response.designatedCount
                    self.urgentCount = 0
                    self.highCount = 0
                    for summary in response.sessions {
                        switch summary.designation.priority {
                        case .urgent:
                            self.urgentCount += 1
                        case .high:
                            self.highCount += 1
                        default:
                            break
                        }
                    }
                    self.hasUrgentSessions = self.urgentCount > 0
                    self.hasHighPrioritySessions = self.highCount > 0
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
