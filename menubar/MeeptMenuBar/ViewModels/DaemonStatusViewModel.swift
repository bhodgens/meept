//
//  DaemonStatusViewModel.swift
//  MeeptMenuBar
//

import AppKit
import Combine
import SwiftUI
import os.log

@MainActor
class DaemonStatusViewModel: ObservableObject {
    @Published var daemonStatus = DaemonStatus()
    @Published var isRefreshingStatus = false
    @Published var isControllingDaemon = false

    var onStatusChanged: (() -> Void)?

    private let apiClient: APIClient
    private let daemonController: DaemonController
    private var statusTimer: Timer?
    private let logger = Logger(subsystem: "com.caimlas.meept.menubar", category: "DaemonStatusViewModel")

    init(apiClient: APIClient, daemonController: DaemonController) {
        self.apiClient = apiClient
        self.daemonController = daemonController
    }

    // MARK: - Polling

    func startPolling() {
        statusTimer?.invalidate()
        // Use Timer(timeInterval:...) + RunLoop.common so the timer continues
        // to fire during modal interactions (e.g. NSMenu tracking, scroll) the
        // way .common-priority work does. Timer.scheduledTimer only attaches
        // to .default, which pauses during menu tracking.
        let timer = Timer(timeInterval: 5.0, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.refreshStatus()
            }
        }
        RunLoop.main.add(timer, forMode: .common)
        statusTimer = timer
        refreshStatus()
    }

    func stopPolling() {
        statusTimer?.invalidate()
        statusTimer = nil
    }

    // MARK: - Status

    func refreshStatus() {
        guard !isRefreshingStatus else { return }
        isRefreshingStatus = true
        Task { [weak self] in
            guard let self else { return }
            do {
                daemonStatus = try await apiClient.getDaemonStatus()
                onStatusChanged?()
            } catch {
                logger.error("failed to fetch daemon status: \(error.localizedDescription)")
            }
            isRefreshingStatus = false
        }
    }

    // MARK: - Control

    func startDaemon() {
        guard !isControllingDaemon else { return }
        isControllingDaemon = true
        Task { [weak self] in
            guard let self else { return }
            do {
                try await daemonController.startDaemon()
            } catch {
                logger.error("failed to start daemon: \(error.localizedDescription)")
            }
            isControllingDaemon = false
            refreshStatus()
        }
    }

    func stopDaemon() {
        guard !isControllingDaemon else { return }
        isControllingDaemon = true
        Task { [weak self] in
            guard let self else { return }
            do {
                try await daemonController.stopDaemon()
            } catch {
                logger.error("failed to stop daemon: \(error.localizedDescription)")
            }
            isControllingDaemon = false
            refreshStatus()
        }
    }

    func restartDaemon() {
        guard !isControllingDaemon else { return }
        isControllingDaemon = true
        Task { [weak self] in
            guard let self else { return }
            do {
                try await daemonController.restartDaemon()
            } catch {
                logger.error("failed to restart daemon: \(error.localizedDescription)")
            }
            isControllingDaemon = false
            refreshStatus()
        }
    }

    // MARK: - Helpers

    var statusImage: NSImage? {
        let symbolName: String
        switch daemonStatus.state {
        case .offline: symbolName = "power"
        case .idle: symbolName = "checkmark.circle"
        case .working: symbolName = "gearshape.2.fill"
        case .error: symbolName = "exclamationmark.triangle.fill"
        }

        if let image = NSImage(systemSymbolName: symbolName, accessibilityDescription: "Meept") {
            image.isTemplate = true
            image.size = NSSize(width: 14, height: 14)
            return image
        }
        return nil
    }
}
