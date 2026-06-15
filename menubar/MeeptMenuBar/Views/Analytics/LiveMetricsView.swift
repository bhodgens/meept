//
//  LiveMetricsView.swift
//  MeeptMenuBar
//

import SwiftUI

@MainActor
struct LiveMetricsView: View {
    @ObservedObject var metricsViewModel: MetricsViewModel

    private let relativeFormatter = RelativeDateTimeFormatter()

    var body: some View {
        VStack(spacing: 16) {
            HStack {
                Text("live metrics")
                    .font(.headline)
                Spacer()
                if metricsViewModel.isLoadingLive {
                    ProgressView()
                        .scaleEffect(0.8)
                }
                if let lastUpdated = metricsViewModel.lastUpdated {
                    Text("updated \(timeAgo(from: lastUpdated))")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            }

            if let errorMessage = metricsViewModel.errorMessage {
                Text(errorMessage)
                    .font(.caption)
                    .foregroundColor(.red)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }

            if let metrics = metricsViewModel.liveMetrics {
                LazyVGrid(columns: [
                    GridItem(.flexible()),
                    GridItem(.flexible())
                ], spacing: 12) {
                    MetricCard(
                        title: "active agents",
                        value: "\(metrics.active_agents)",
                        icon: "cpu",
                        color: .blue
                    )
                    MetricCard(
                        title: "requests/sec",
                        value: String(format: "%.1f", metrics.requests_per_sec),
                        icon: "arrow.triangle.2.circlepath",
                        color: .green
                    )
                    MetricCard(
                        title: "tokens/sec",
                        value: String(format: "%.0f", metrics.token_usage_rate),
                        icon: "arrow.up.right",
                        color: .purple
                    )
                    MetricCard(
                        title: "queue depth",
                        value: "\(metrics.queue_depth)",
                        icon: "list.bullet.rectangle",
                        color: .orange
                    )
                    MetricCard(
                        title: "failovers",
                        value: "\(metrics.model_failovers)",
                        icon: "exclamationmark.triangle",
                        color: metrics.model_failovers > 0 ? .red : .gray
                    )
                }
            } else {
                Text("no metrics available")
                    .foregroundColor(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }

            Spacer()
        }
        .padding()
        .onAppear {
            metricsViewModel.startLivePolling()
        }
        .onDisappear {
            metricsViewModel.stopLivePolling()
        }
    }

    private func timeAgo(from date: Date) -> String {
        relativeFormatter.dateTimeStyle = .named
        return relativeFormatter.localizedString(for: date, relativeTo: Date())
    }
}

struct MetricCard: View {
    let title: String
    let value: String
    let icon: String
    let color: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Image(systemName: icon)
                    .foregroundColor(color)
                Spacer()
            }
            Text(value)
                .font(.title2)
                .fontWeight(.bold)
            Text(title)
                .font(.caption)
                .foregroundColor(.secondary)
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(color.opacity(0.1))
        .cornerRadius(8)
    }
}

#Preview {
    LiveMetricsView(metricsViewModel: MetricsViewModel(dashboardService: DashboardService()))
}
