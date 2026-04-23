//
//  AppDelegate.swift
//  MeeptMenuBar
//

import SwiftUI
import AppKit

@main
class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem?
    private var popover: NSPopover?

    private let apiClient = APIClient()
    private let daemonController = DaemonController()
    private var daemonStatus = DaemonStatus()
    private var isUpdating = false

    func applicationDidFinishLaunching(_ notification: Notification) {
        setupStatusItem()
        startStatusPolling()
    }

    private func setupStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)

        if let button = statusItem?.button {
            button.image = NSImage(systemSymbolName: "cpu", accessibilityDescription: "Meept")
            button.action = #selector(togglePopover(_:))
            button.target = self
        }

        popover = NSPopover()
        popover?.contentSize = NSSize(width: 200, height: 150)
        popover?.behavior = .transient
        updatePopoverContent()
    }

    private func updatePopoverContent() {
        popover?.contentViewController = NSHostingController(
            rootView: MenuView(
                daemonStatus: daemonStatus,
                onStart: { [weak self] in self?.startDaemon() },
                onStop: { [weak self] in self?.stopDaemon() },
                onRestart: { [weak self] in self?.restartDaemon() },
                onQuit: { NSApp.terminate(nil) }
            )
        )
    }

    @objc private func togglePopover(_ sender: AnyObject?) {
        if let button = statusItem?.button {
            if popover?.isShown == true {
                popover?.performClose(sender)
            } else {
                popover?.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
            }
        }
    }

    private func startStatusPolling() {
        Timer.scheduledTimer(withTimeInterval: 5.0, repeats: true) { [weak self] _ in
            self?.fetchDaemonStatus()
        }
        fetchDaemonStatus()
    }

    private func fetchDaemonStatus() {
        guard !isUpdating else { return }
        apiClient.getDaemonStatus { [weak self] result in
            DispatchQueue.main.async {
                if case .success(let status) = result {
                    self?.daemonStatus = status
                    self?.updatePopoverContent()
                }
            }
        }
    }

    private func startDaemon() {
        guard !isUpdating else { return }
        isUpdating = true
        daemonController.startDaemon { [weak self] _ in
            DispatchQueue.main.async {
                self?.isUpdating = false
                self?.fetchDaemonStatus()
            }
        }
    }

    private func stopDaemon() {
        guard !isUpdating else { return }
        isUpdating = true
        daemonController.stopDaemon { [weak self] _ in
            DispatchQueue.main.async {
                self?.isUpdating = false
                self?.fetchDaemonStatus()
            }
        }
    }

    private func restartDaemon() {
        guard !isUpdating else { return }
        isUpdating = true
        daemonController.restartDaemon { [weak self] _ in
            DispatchQueue.main.async {
                self?.isUpdating = false
                self?.fetchDaemonStatus()
            }
        }
    }
}
