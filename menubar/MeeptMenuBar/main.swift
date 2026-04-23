import AppKit
import SwiftUI
import os.log

class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem?
    private var popover: NSPopover?
    
    private let apiClient = APIClient()
    private let daemonController = DaemonController()
    private var daemonStatus = DaemonStatus()
    private var isUpdating = false
    private let logger = Logger(subsystem: "com.caimlas.meept.menubar", category: "Main")
    
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
        popover?.contentSize = NSSize(width: 200, height: 150)
        popover?.behavior = .transient
        updatePopoverContent()
        
        startStatusPolling()
    }
    
    private func createStatusImage() -> NSImage? {
        let symbolName: String
        switch daemonStatus.state {
        case .offline:
            symbolName = "power"
        case .idle:
            symbolName = "checkmark.circle"
        case .working:
            symbolName = "gearshape.2.fill"
        case .error:
            symbolName = "exclamationmark.triangle.fill"
        }
        
        if let image = NSImage(systemSymbolName: symbolName, accessibilityDescription: "Meept") {
            image.isTemplate = true
            image.size = NSSize(width: 14, height: 14)
            return image
        }
        return nil
    }
    
    private func updateStatusImage() {
        if let image = createStatusImage() {
            statusItem?.button?.image = image
        }
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
                    self?.updateStatusImage()
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

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
