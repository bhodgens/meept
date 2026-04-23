//
//  LiveMetricsView.swift
//  MeeptMenuBar
//

import SwiftUI

struct LiveMetricsView: View {
    @State private var metrics: LiveMetrics?
    @State private var isLoading = false
    @State private var lastUpdated: Date?
    
    private let dashboardService = DashboardService()
    private let updateInterval: TimeInterval = 5.0
    
    var body: some View {
        VStack(spacing: 16) {
            HStack {
                Text("live metrics")
                    .font(.headline)
                Spacer()
                if isLoading {
                    ProgressView()
                        .scaleEffect(0.8)
                }
                if let lastUpdated = lastUpdated {
                    Text("updated \(timeAgo(from: lastUpdated))")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            }
            
            if let metrics = metrics {
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
            startPolling()
        }
        .onDisappear {
            stopPolling()
        }
    }
    
    private func startPolling() {
        fetchMetrics()
        Timer.scheduledTimer(withTimeInterval: updateInterval, repeats: true) { _ in
            fetchMetrics()
        }
    }
    
    private func stopPolling() {
        // Timers will stop when run loop ends
    }
    
    private func fetchMetrics() {
        isLoading = true
        dashboardService.getLiveMetrics { result in
            DispatchQueue.main.async {
                switch result {
                case .success(let metrics):
                    self.metrics = metrics
                    self.lastUpdated = Date()
                case .failure:
                    break
                }
                isLoading = false
            }
        }
    }
    
    private func timeAgo(from date: Date) -> String {
        let interval = Date().timeIntervalSince(date)
        if interval < 1 { return "just now" }
        if interval < 60 { return "\(Int(interval))s ago" }
        if interval < 3600 { return "\(Int(interval/60))m ago" }
        return "\(Int(interval/3600))h ago"
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
    LiveMetricsView()
}
