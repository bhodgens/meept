//
//  NotificationManager.swift
//  MeeptMenuBar
//

import Foundation
import UserNotifications
import Combine

class NotificationManager: NSObject, ObservableObject {
    static let shared = NotificationManager()

    private let notificationCenter = UNUserNotificationCenter.current()
    private var websocket: WebSocketManager?
    private var lastNotificationTime: Date = Date.distantPast
    // Cached once so WebSocket setup and per-event filtering read the same
    // snapshot instead of re-parsing menubar.json5 on every notification.
    private let configService: MenubarConfigService

    @Published var notifications: [NotificationEvent] = []
    @Published var isEnabled: Bool = true

    struct NotificationEvent: Codable, Identifiable {
        let id: String
        let timestamp: String
        let type: String  // "info", "success", "warning", "error"
        let title: String
        let message: String
        let taskID: String?
        let agentID: String?
        var isRead: Bool = false
    }

    private override init() {
        self.configService = MenubarConfigService()
        super.init()
        requestAuthorization()
        setupWebSocket()
    }

    // MARK: - Authorization

    func requestAuthorization() {
        notificationCenter.requestAuthorization(options: [.alert, .sound, .badge]) { [weak self] granted, error in
            DispatchQueue.main.async {
                self?.isEnabled = granted
                if let error = error {
                    print("Notification authorization error: \(error)")
                }
            }
        }
        notificationCenter.delegate = self
    }

    // MARK: - WebSocket Setup

    private func setupWebSocket() {
        // Use wss:// for secure WebSocket; daemon should handle TLS
        let wsURL = configService.daemonBaseURL
            .replacingOccurrences(of: "https://", with: "wss://")
            .replacingOccurrences(of: "http://", with: "ws://")

        websocket = WebSocketManager(url: wsURL, apiToken: configService.apiToken)

        websocket?.onMessage = { [weak self] data in
            self?.handleWebSocketMessage(data)
        }

        websocket?.onDisconnect = { error in
            print("Notification WebSocket disconnected: \(error?.localizedDescription ?? "unknown")")
            // Reconnection is handled by WebSocketManager
        }

        websocket?.onConnect = {
            print("Notification WebSocket connected")
        }

        websocket?.connect()
    }

    // MARK: - WebSocket Message Handling

    private func handleWebSocketMessage(_ data: Data) {
        do {
            let event = try JSONDecoder().decode(NotificationEvent.self, from: data)
            DispatchQueue.main.async { [weak self] in
                self?.notifications.insert(event, at: 0)
                self?.showLocalNotification(event)
            }
        } catch {
            print("Failed to decode notification event: \(error)")
        }
    }

    // MARK: - Local Notifications

    func showLocalNotification(_ event: NotificationEvent) {
        // Check notification level filter
        let level = configService.notificationsLevel
        switch level {
        case "none":
            return
        case "errors_only":
            guard event.type == "error" else { return
            }
        case "warnings_and_above":
            guard ["error", "warning"].contains(event.type) else { return
            }
        case "all":
            break
        default:
            break
        }

        // Rate limit: don't show notifications more than once per second
        let now = Date()
        guard now.timeIntervalSince(lastNotificationTime) > 1.0 else { return }
        lastNotificationTime = now

        let content = UNMutableNotificationContent()
        content.title = event.title
        content.body = event.message

        if let sound = getSound(for: event.type) {
            content.sound = sound
        } else {
            content.sound = .default
        }

        // Add category for action handling
        content.categoryIdentifier = "MEEPT_NOTIFICATION"

        // Add user info for tap handling
        content.userInfo = [
            "eventID": event.id,
            "type": event.type,
            "taskID": event.taskID ?? "",
            "agentID": event.agentID ?? ""
        ]

        let request = UNNotificationRequest(
            identifier: event.id,
            content: content,
            trigger: nil  // Deliver immediately
        )

        notificationCenter.add(request) { error in
            if let error = error {
                print("Failed to show notification: \(error)")
            }
        }
    }

    func getSound(for type: String) -> UNNotificationSound? {
        switch type {
        case "error":
            return UNNotificationSound.defaultCritical
        case "warning":
            return UNNotificationSound.default
        case "success":
            // Use default sound for success
            return nil
        default:
            return nil
        }
    }

    // MARK: - Notification Management

    func clearNotifications() {
        notifications.removeAll()
        notificationCenter.removeAllDeliveredNotifications()
    }

    func markAsRead(_ eventID: String) {
        if let index = notifications.firstIndex(where: { $0.id == eventID }) {
            notifications[index].isRead = true
        }
    }

    func removeNotification(_ eventID: String) {
        notifications.removeAll { $0.id == eventID }
        notificationCenter.removeDeliveredNotifications(withIdentifiers: [eventID])
    }

    // MARK: - Toggle

    func setEnabled(_ enabled: Bool) {
        isEnabled = enabled
        if !enabled {
            notificationCenter.removeAllPendingNotificationRequests()
        } else {
            requestAuthorization()
        }
    }
}

// MARK: - UNUserNotificationCenterDelegate

extension NotificationManager: UNUserNotificationCenterDelegate {
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        // Show notification even when app is in foreground
        completionHandler([.banner, .sound])
    }

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let userInfo = response.notification.request.content.userInfo

        if let eventID = userInfo["eventID"] as? String {
            markAsRead(eventID)
        }

        // Handle notification tap - could open relevant view
        if let taskID = userInfo["taskID"] as? String, !taskID.isEmpty {
            print("Notification tapped for task: \(taskID)")
            // Post notification for other parts of the app to handle
            NotificationCenter.default.post(
                name: .notificationTapped,
                object: nil,
                userInfo: userInfo
            )
        }

        completionHandler()
    }
}

// MARK: - Notification Names

extension Notification.Name {
    static let notificationTapped = Notification.Name("com.meept.notificationTapped")
}