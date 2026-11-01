//
//  GuiLauncher.swift
//  MeeptMenuBar
//
//  Single launch/focus path for the meept GUI app. The notification
//  tap-through (NotificationManager) opens the GUI with a meept://session/
//  deep link through this launcher; nothing else may spawn the GUI.
//

import AppKit
import os.log

enum GuiLauncher {
    private static let logger = Logger(
        subsystem: "com.caimlas.meept.menubar", category: "GuiLauncher")

    /// Open (or focus) the meept GUI with [url].
    ///
    /// NSWorkspace.open drives BOTH cases: the GUI registers the meept
    /// scheme in its Info.plist (CFBundleURLTypes), so a full URL opens the
    /// app and delivers the link; when the app is already running the same
    /// call activates it and fires the openURLs event (warm path).
    ///
    /// Returns true when the launch was handed to the system successfully.
    @discardableResult
    static func open(_ url: URL) -> Bool {
        logger.info("launching gui with \(url.absoluteString, privacy: .public)")
        let ok = NSWorkspace.shared.open(url)
        if !ok {
            logger.error("gui launch failed for \(url.absoluteString, privacy: .public)")
        }
        return ok
    }
}
