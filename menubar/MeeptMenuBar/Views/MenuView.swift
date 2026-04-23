//
//  MenuView.swift
//  MeeptMenuBar
//

import SwiftUI

struct MenuView: View {
    let daemonStatus: DaemonStatus
    let onStart: () -> Void
    let onStop: () -> Void
    let onRestart: () -> Void
    let onShowSettings: () -> Void
    let onShowDashboard: () -> Void
    let onQuit: () -> Void

    private var stateIcon: String {
        switch daemonStatus.state {
        case .offline: return "power"
        case .idle: return "checkmark.circle"
        case .working: return "gearshape.2.fill"
        case .error: return "exclamationmark.triangle.fill"
        }
    }

    private var stateColor: Color {
        switch daemonStatus.state {
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
                    Text(daemonStatus.state.rawValue)
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
                Spacer()
            }
            .padding(12)

            Divider()

            // Daemon control
            if daemonStatus.running {
                Button("stop daemon", action: onStop)
                    .buttonStyle(.plain)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)

                Button("restart daemon", action: onRestart)
                    .buttonStyle(.plain)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
            } else {
                Button("start daemon", action: onStart)
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
    MenuView(
        daemonStatus: DaemonStatus(running: true, pid: 123, uptime: "1h", state: .idle),
        onStart: {},
        onStop: {},
        onRestart: {},
        onShowSettings: {},
        onShowDashboard: {},
        onQuit: {}
    )
}
