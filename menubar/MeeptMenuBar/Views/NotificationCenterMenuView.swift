//
//  NotificationCenterMenuView.swift
//  MeeptMenuBar
//

import SwiftUI

struct NotificationCenterMenuView: View {
    @StateObject private var notificationManager = NotificationManager.shared
    @State private var expanded = false
    @State private var doNotDisturb = MenubarConfigService().doNotDisturb

    var body: some View {
        Menu {
            if notificationManager.notifications.isEmpty {
                Text("no notifications")
                    .foregroundColor(.secondary)
            } else {
                ForEach(notificationManager.notifications) { notification in
                    NotificationRowView(notification: notification)
                }
                Divider()
                Button("clear all") {
                    notificationManager.clearNotifications()
                }
            }
            Divider()
            Toggle("enable notifications", isOn: $notificationManager.isEnabled)
            Toggle("do not disturb", isOn: Binding(
                get: { doNotDisturb },
                set: { newValue in
                    doNotDisturb = newValue
                    MenubarConfigService().setDoNotDisturb(newValue)
                }
            ))
        } label: {
            ZStack {
                Image(systemName: doNotDisturb ? "bell.slash" : "bell")
                if !doNotDisturb && !notificationManager.notifications.isEmpty {
                    Circle()
                        .fill(.red)
                        .frame(width: 8, height: 8)
                        .offset(x: 4, y: -4)
                }
            }
        }
    }
}

struct NotificationRowView: View {
    let notification: NotificationManager.NotificationEvent
    @StateObject private var notificationManager = NotificationManager.shared

    private var typeIcon: String {
        switch notification.type {
        case "error":
            return "exclamationmark.triangle.fill"
        case "warning":
            return "exclamationmark.circle.fill"
        case "success":
            return "checkmark.circle.fill"
        case "info":
            return "info.circle.fill"
        default:
            return "bell.fill"
        }
    }

    private var typeColor: Color {
        switch notification.type {
        case "error":
            return .red
        case "warning":
            return .orange
        case "success":
            return .green
        case "info":
            return .blue
        default:
            return .secondary
        }
    }

    private var timeAgo: String {
        guard let date = ISO8601DateFormatter().date(from: notification.timestamp) else {
            return ""
        }
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .abbreviated
        return formatter.localizedString(for: date, relativeTo: Date())
    }

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: typeIcon)
                .foregroundColor(typeColor)
                .frame(width: 20)

            VStack(alignment: .leading, spacing: 2) {
                Text(notification.title)
                    .font(.system(size: 12, weight: .semibold))
                    .lineLimit(1)

                Text(notification.message)
                    .font(.system(size: 11))
                    .foregroundColor(.secondary)
                    .lineLimit(2)
            }

            Spacer()

            VStack(alignment: .trailing, spacing: 2) {
                Text(timeAgo)
                    .font(.system(size: 10))
                    .foregroundColor(.secondary)

                Button(action: {
                    notificationManager.removeNotification(notification.id)
                }) {
                    Image(systemName: "xmark")
                        .font(.system(size: 10))
                        .foregroundColor(.secondary)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.vertical, 4)
        .frame(maxWidth: 280)
    }
}

#Preview {
    NotificationCenterMenuView()
}