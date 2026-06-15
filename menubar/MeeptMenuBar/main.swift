import AppKit
import SwiftUI
import os.log

@MainActor
class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem?
    private var popover: NSPopover?
    private var settingsWindow: NSWindow?
    private var dashboardWindow: NSWindow?

    private let daemonStatusVM: DaemonStatusViewModel
    private let configVM: ConfigViewModel
    private let metricsVM: MetricsViewModel
    private let logger = Logger(subsystem: "com.caimlas.meept.menubar", category: "Main")

    override init() {
        let menubarConfig = MenubarConfigService()
        let apiClient = APIClient(
            baseURL: menubarConfig.daemonBaseURL,
            apiToken: menubarConfig.apiToken
        )
        let daemonController = DaemonController()
        self.daemonStatusVM = DaemonStatusViewModel(apiClient: apiClient, daemonController: daemonController)
        self.configVM = ConfigViewModel(configService: ConfigService())
        self.metricsVM = MetricsViewModel(dashboardService: DashboardService())
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        logger.info("applicationDidFinishLaunching called!")

        NSApp.setActivationPolicy(.accessory)

        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        statusItem?.behavior = .removalAllowed

        if let button = statusItem?.button {
            button.title = "meept"

            if let cpuImage = NSImage(systemSymbolName: "cpu", accessibilityDescription: "Meept") {
                cpuImage.isTemplate = true
                cpuImage.size = NSSize(width: 14, height: 14)
                button.image = cpuImage
            }
            button.action = #selector(togglePopover(_:))
            button.target = self
            logger.info("button configured with title: \(button.title)")
        }

        popover = NSPopover()
        popover?.contentSize = NSSize(width: 220, height: 180)
        popover?.behavior = .transient
        popover?.contentViewController = NSHostingController(
            rootView: MenuBarContentView(
                daemonStatusVM: daemonStatusVM,
                onShowSettings: { [weak self] in self?.showSettings() },
                onShowDashboard: { [weak self] in self?.showDashboard() },
                onQuit: { NSApp.terminate(nil) }
            )
        )

        // Update the menubar icon whenever daemon status changes
        daemonStatusVM.onStatusChanged = { [weak self] in
            if let image = self?.daemonStatusVM.statusImage {
                self?.statusItem?.button?.image = image
            }
        }

        daemonStatusVM.startPolling()
    }

    func applicationWillTerminate(_ notification: Notification) {
        daemonStatusVM.stopPolling()
    }

    private func showSettings() {
        if settingsWindow == nil {
            let hostingController = NSHostingController(rootView: SettingsWindow(configViewModel: configVM))
            let window = NSWindow(
                contentRect: NSRect(x: 0, y: 0, width: 600, height: 450),
                styleMask: [.titled, .closable, .resizable],
                backing: .buffered,
                defer: false
            )
            window.contentViewController = hostingController
            window.title = "meept settings"
            window.isReleasedWhenClosed = false
            window.center()
            settingsWindow = window
        }

        settingsWindow?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    private func showDashboard() {
        if dashboardWindow == nil {
            let hostingController = NSHostingController(rootView: DashboardWindow(metricsViewModel: metricsVM))
            let window = NSWindow(
                contentRect: NSRect(x: 0, y: 0, width: 500, height: 400),
                styleMask: [.titled, .closable, .resizable],
                backing: .buffered,
                defer: false
            )
            window.contentViewController = hostingController
            window.title = "meept dashboard"
            window.isReleasedWhenClosed = false
            window.center()
            dashboardWindow = window
        }

        dashboardWindow?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
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
}

let app = NSApplication.shared
// NSApplication.delegate is weak; keep a strong reference for the lifetime of app.run().
let appDelegate = MainActor.assumeIsolated { AppDelegate() }
app.delegate = appDelegate
app.run()
