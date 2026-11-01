//
//  DeepLinkChannel.swift
//  Runner (meept gui, macOS)
//
//  Registers the meept:// URL scheme into the Dart listener
//  (hitl-notifications leaf 03, T4).
//
//  The engine's FlutterAppDelegate implements application:openURLs: and
//  forwards it to registered FlutterAppLifecycleDelegates via
//  handleOpenURLs: (verified on this host's FlutterMacOS.framework), so a
//  lifecycle delegate + a MethodChannel is the whole bridge:
//
//    cold start  -> NotificationCenter observer (application:openURLs: is
//                   delivered as a notification before the channel exists)
//    warm link   -> handleOpenURLs: through the lifecycle registrar
//    both        -> channel method onDeepLink(String)
//    cold-start pull -> Dart calls getInitialLink at startup
//
//  Dart side: ui/flutter_ui/lib/core/deep_link.dart
//  (initDesktopDeepLinkChannel). Invalid URLs are ignored there with a
//  lowercase log line.
//

import Cocoa
import FlutterMacOS

/// Bridge object keeping the lifecycle delegate alive (the registrar holds
/// delegates weakly) and observing the openURLs notification for
/// cold-start delivery.
class DeepLinkChannel {
    static let channelName = "com.caimlas.meept/deep_link"
    static let openURLsNotification =
        NSNotification.Name("NSApplicationDelegate.application.openURLs.self")

    private var pendingURL: String?
    private let handler: DeepLinkLifecycleDelegate
    private weak var channel: FlutterMethodChannel?

    /// Two-phase init seam: the lifecycle delegate's closure exists before
    /// DeepLinkChannel's stored properties are set, so it hands URLs to the
    /// box first and the box forwards once self is live.
    private final class BridgeBox {
        var deliver: (([URL]) -> Void)?
    }

    init(messenger: FlutterBinaryMessenger) {
        // The closure captures the BridgeBox (not self) so `handler` can be
        // assigned first without a use-before-init cycle.
        let box = BridgeBox()
        handler = DeepLinkLifecycleDelegate { urls in
            box.deliver?(urls)
        }
        box.deliver = { [weak self] urls in
            self?.deliver(urls)
        }

        // Warm path: engine-forwarded application:openURLs:.
        if let app = NSApplication.shared.delegate as? FlutterAppDelegate {
            app.addApplicationLifecycleDelegate(handler)
        }

        // Cold-start path: application:openURLs: can fire (as an
        // NSWorkSpace/NSApp notification) before awakeFromNib finishes and
        // before the Dart side registers; stash the URL so the Dart
        // getInitialLink pull finds it.
        NotificationCenter.default.addObserver(
            forName: DeepLinkChannel.openURLsNotification,
            object: nil,
            queue: .main
        ) { [weak self] notification in
            if let urls = notification.object as? [URL] {
                self?.deliver(urls)
            }
        }

        let channel = FlutterMethodChannel(name: DeepLinkChannel.channelName,
                                           binaryMessenger: messenger)
        self.channel = channel
        channel.setMethodCallHandler { [weak self] call, result in
            switch call.method {
            case "getInitialLink":
                result(self?.pendingURL)
                self?.pendingURL = nil
            default:
                result(FlutterMethodNotImplemented)
            }
        }
    }

    /// Deliver URLs: push over the channel when Dart is listening, else
    /// stash for the getInitialLink pull.
    private func deliver(_ urls: [URL]) {
        guard let first = urls.first else { return }
        let link = first.absoluteString
        if let channel = channel {
            channel.invokeMethod("onDeepLink", arguments: link)
        } else {
            pendingURL = link
        }
    }
}

/// Lifecycle delegate forwarding the engine's openURLs callback into the
/// channel. Returns YES for meept:// URLs (consumed) so no other delegate
/// re-handles them, NO otherwise.
class DeepLinkLifecycleDelegate: NSObject, FlutterAppLifecycleDelegate {
    private let onURLs: ([URL]) -> Void

    init(onURLs: @escaping ([URL]) -> Void) {
        self.onURLs = onURLs
        super.init()
    }

    func handleOpen(_ urls: [URL]) -> Bool {
        guard urls.contains(where: { $0.scheme?.lowercased() == "meept" }) else {
            return false
        }
        onURLs(urls)
        return true
    }
}
