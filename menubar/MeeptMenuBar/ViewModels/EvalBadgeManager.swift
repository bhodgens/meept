//
//  EvalBadgeManager.swift
//  MeeptMenuBar
//

import Foundation
import AppKit
import os.log

@MainActor
class EvalBadgeManager: ObservableObject {
    @Published var failCount: Int = 0
    @Published var lastFail: String?

    private let apiClient: APIClient
    private var badgeTimer: Timer?
    private let logger = Logger(subsystem: "com.caimlas.meept.menubar", category: "EvalBadgeManager")

    init(apiClient: APIClient) {
        self.apiClient = apiClient
    }

    func startPolling(interval: TimeInterval = 30.0) {
        badgeTimer?.invalidate()
        let timer = Timer(timeInterval: interval, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.fetchEvalRuns()
            }
        }
        RunLoop.main.add(timer, forMode: .common)
        badgeTimer = timer
        fetchEvalRuns()
    }

    func stopPolling() {
        badgeTimer?.invalidate()
        badgeTimer = nil
    }

    func fetchEvalRuns() {
        Task { [weak self] in
            guard let self else { return }
            do {
                let response = try await apiClient.listEvalRuns()
                DispatchQueue.main.async {
                    var fails = 0
                    var lastFailText: String?
                    for run in response.runs {
                        let anyFailed = run.outcomes.contains { !$0.passed }
                        if anyFailed {
                            fails += 1
                            lastFailText = "\(run.task_id) \(self.timeAgo(for: run.id))"
                        }
                    }
                    self.failCount = fails
                    self.lastFail = lastFailText
                }
            } catch {
                logger.error("failed to fetch eval runs: \(error.localizedDescription)")
            }
        }
    }

    private func timeAgo(for timestamp: String) -> String {
        let formatter = ISO8601DateFormatter()
        guard let date = formatter.date(from: timestamp) else { return "" }
        let rel = RelativeDateTimeFormatter()
        rel.unitsStyle = .abbreviated
        return rel.localizedString(for: date, relativeTo: Date())
    }
}
