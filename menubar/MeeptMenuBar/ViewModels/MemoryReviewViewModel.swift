//
//  MemoryReviewViewModel.swift
//  MeeptMenuBar
//
//  View model for the "memory" tab in Settings. Polls
//  `GET /api/v1/memory/review-queue` every 5s while visible and supports
//  optimistic promote/reject of auto-claims via POST endpoints.
//

import Combine
import SwiftUI
import os.log

@MainActor
class MemoryReviewViewModel: ObservableObject {
    @Published var autoClaims: [MemoryResult] = []
    @Published var pendingDecisions: [MemoryResult] = []
    @Published var pendingPredictions: [MemoryResult] = []
    @Published var isLoading = false
    @Published var errorMessage: String?
    @Published var showError = false

    private let api: APIClient
    private var pollTimer: Timer?
    private let updateInterval: TimeInterval = 5.0
    /// Claim IDs with an in-flight promote/reject. Used to disable buttons in
    /// the UI so the user cannot double-click. MainActor-only so no lock needed.
    private var pendingActions: Set<String> = []
    private let logger = Logger(subsystem: "com.caimlas.meept.menubar", category: "MemoryReviewViewModel")

    init(api: APIClient) {
        self.api = api
    }

    // MARK: - Polling

    func startPolling() {
        refresh()
        pollTimer?.invalidate()
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
        guard !isLoading else { return }
        isLoading = true
        Task { [weak self] in
            guard let self else { return }
            do {
                let result = try await api.getReviewQueue()
                autoClaims = result.autoClaims
                pendingDecisions = result.pendingDecisions
                pendingPredictions = result.pendingPredictions
                errorMessage = nil
            } catch {
                logger.error("failed to fetch review queue: \(error.localizedDescription)")
                errorMessage = error.localizedDescription
            }
            isLoading = false
        }
    }

    // MARK: - Promote / Reject (optimistic)

    func promote(_ claim: MemoryResult) async {
        let id = claim.id
        // Optimistic update: remove from local list immediately.
        autoClaims.removeAll { $0.id == id }
        pendingActions.insert(id)
        defer { pendingActions.remove(id) }
        do {
            try await api.promoteClaim(id: id)
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
            showError = true
            refresh()
        }
    }

    func reject(_ claim: MemoryResult) async {
        let id = claim.id
        autoClaims.removeAll { $0.id == id }
        pendingActions.insert(id)
        defer { pendingActions.remove(id) }
        do {
            try await api.rejectClaim(id: id)
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
            showError = true
            refresh()
        }
    }

    func isPending(_ claim: MemoryResult) -> Bool {
        pendingActions.contains(claim.id)
    }
}
