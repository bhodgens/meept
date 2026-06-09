//
//  MetricsViewModel.swift
//  MeeptMenuBar
//

import Combine
import SwiftUI
import os.log

@MainActor
class MetricsViewModel: ObservableObject {
    @Published var liveMetrics: LiveMetrics?
    @Published var historicalData: [MetricPoint] = []
    @Published var isLoadingLive = false
    @Published var isLoadingHistorical = false
    @Published var lastUpdated: Date?

    // Historical query state
    @Published var fromDate = Date().addingTimeInterval(-3600 * 24)
    @Published var toDate = Date()
    @Published var resolution = "hour"

    private let dashboardService: DashboardService
    private var metricsTimer: Timer?
    private let updateInterval: TimeInterval = 5.0
    private let logger = Logger(subsystem: "com.caimlas.meept.menubar", category: "MetricsViewModel")

    init(dashboardService: DashboardService) {
        self.dashboardService = dashboardService
    }

    // MARK: - Live Metrics Polling

    func startLivePolling() {
        fetchLiveMetrics()
        metricsTimer?.invalidate()
        metricsTimer = Timer.scheduledTimer(withTimeInterval: updateInterval, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.fetchLiveMetrics()
            }
        }
    }

    func stopLivePolling() {
        metricsTimer?.invalidate()
        metricsTimer = nil
    }

    func fetchLiveMetrics() {
        isLoadingLive = true
        Task { [weak self] in
            guard let self else { return }
            do {
                liveMetrics = try await dashboardService.getLiveMetrics()
                lastUpdated = Date()
            } catch {
                // Silently ignore metric fetch errors; next poll will retry
            }
            isLoadingLive = false
        }
    }

    // MARK: - Historical Metrics

    func fetchHistorical() {
        isLoadingHistorical = true
        Task { [weak self] in
            guard let self else { return }
            do {
                let formatter = ISO8601DateFormatter()
                let fromISO = formatter.string(from: fromDate)
                let toISO = formatter.string(from: toDate)
                historicalData = try await dashboardService.getHistoricalMetrics(
                    from: fromISO, to: toISO, resolution: resolution
                )
            } catch {
                logger.error("failed to fetch historical metrics: \(error.localizedDescription)")
            }
            isLoadingHistorical = false
        }
    }
}
