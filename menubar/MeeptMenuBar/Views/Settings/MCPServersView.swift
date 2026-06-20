//
//  MCPServersView.swift
//  MeeptMenuBar
//
//  SwiftUI Table view for browsing and toggling MCP servers.
//  All UI text is lowercase per CLAUDE.md UI convention.
//

import SwiftUI

@MainActor
struct MCPServersView: View {
    @ObservedObject var viewModel: MCPViewModel

    var body: some View {
        VStack(spacing: 0) {
            Table(viewModel.servers) {
                TableColumn("enabled") { server in
                    Toggle(
                        "",
                        isOn: Binding(
                            get: { server.enabled },
                            set: { _ in
                                guard !viewModel.isPending(server) else { return }
                                Task { await viewModel.toggleEnabled(server) }
                            }
                        )
                    )
                    .disabled(viewModel.isPending(server))
                    .labelsHidden()
                }
                .width(min: 40, ideal: 50, max: 60)

                TableColumn("server") { server in
                    Text(server.name)
                        .help(server.name)
                }

                TableColumn("status") { server in
                    Text(server.state)
                        .foregroundColor(color(for: server.state))
                }

                TableColumn("requests") { server in
                    Text("\(server.requests)")
                        .monospacedDigit()
                }

                TableColumn("errors") { server in
                    Text("\(server.errors)")
                        .monospacedDigit()
                        .foregroundColor(server.errors > 0 ? .red : .primary)
                }

                TableColumn("description") { server in
                    Text(server.description ?? "")
                        .lineLimit(1)
                        .truncationMode(.tail)
                        .foregroundColor(.secondary)
                }
            }
            .tableStyle(.inset(alternatesRowBackgrounds: true))

            Divider()

            HStack(spacing: 8) {
                if viewModel.isLoading {
                    ProgressView()
                        .controlSize(.small)
                }
                Spacer()
                Button(action: { viewModel.refresh() }) {
                    Label("refresh", systemImage: "arrow.clockwise")
                }
                .buttonStyle(.bordered)
                .disabled(viewModel.isLoading)
            }
            .padding(8)
        }
        .alert(
            "mcp toggle failed",
            isPresented: $viewModel.showError
        ) {
            Button("ok", role: .cancel) { }
        } message: {
            Text(viewModel.errorMessage ?? "unknown error")
        }
        .onAppear {
            viewModel.startPolling()
        }
        .onDisappear {
            viewModel.stopPolling()
        }
    }

    /// Per-spec color mapping for server states. Falls back to primary for any
    /// unexpected value so new daemon-side states remain readable.
    private func color(for state: String) -> Color {
        switch state {
        case "active":
            return .green
        case "disabled":
            return .secondary
        case "inactive":
            return .yellow
        case "error":
            return .red
        default:
            return .primary
        }
    }
}

#Preview {
    MCPServersView(
        viewModel: MCPViewModel(
            api: APIClient()
        )
    )
}
