//
//  MenuBarContentView.swift
//  MeeptMenuBar
//

import SwiftUI

struct MenuBarContentView: View {
    @ObservedObject var daemonStatusVM: DaemonStatusViewModel
    @StateObject private var notificationManager = NotificationManager.shared

    let onShowSettings: () -> Void
    let onShowDashboard: () -> Void
    let onQuit: () -> Void

    private var stateIcon: String {
        switch daemonStatusVM.daemonStatus.state {
        case .offline: return "power"
        case .idle: return "checkmark.circle"
        case .working: return "gearshape.2.fill"
        case .error: return "exclamationmark.triangle.fill"
        }
    }

    private var stateColor: Color {
        switch daemonStatusVM.daemonStatus.state {
        case .offline: return .gray
        case .idle: return .green
        case .working: return .blue
        case .error: return .red
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack(spacing: 8) {
                Image(systemName: stateIcon)
                    .foregroundColor(stateColor)
                VStack(alignment: .leading) {
                    Text("meept")
                        .font(.headline)
                    Text(daemonStatusVM.daemonStatus.state.rawValue)
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
                Spacer()

                // Notification bell with badge
                NotificationCenterMenuView()
                    .frame(width: 24, height: 24)
            }
            .padding(12)

            Divider()

            // Daemon control
            if daemonStatusVM.daemonStatus.running {
                Button("stop daemon", action: daemonStatusVM.stopDaemon)
                    .buttonStyle(.plain)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)

                Button("restart daemon", action: daemonStatusVM.restartDaemon)
                    .buttonStyle(.plain)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
            } else {
                Button("start daemon", action: daemonStatusVM.startDaemon)
                    .buttonStyle(.plain)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
            }

            Divider()

            Button("settings", action: onShowSettings)
                .buttonStyle(.plain)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 12)
                .padding(.vertical, 6)

            Button("dashboard", action: onShowDashboard)
                .buttonStyle(.plain)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 12)
                .padding(.vertical, 6)

            Divider()

            Button("quit", action: onQuit)
                .buttonStyle(.plain)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
        }
        .frame(width: 220)
    }
}

#Preview {
    MenuBarContentView(
        daemonStatusVM: DaemonStatusViewModel(
            apiClient: APIClient(baseURL: "https://localhost:8081"),
            daemonController: DaemonController()
        ),
        onShowSettings: {},
        onShowDashboard: {},
        onQuit: {}
    )
}