//
//  DashboardWindow.swift
//  MeeptMenuBar
//

import SwiftUI

struct DashboardWindow: View {
    @State private var selectedTab = 0
    @ObservedObject var metricsViewModel: MetricsViewModel
    @StateObject private var agentsViewModelHolder = AgentsViewModelHolder()

    var body: some View {
        TabView(selection: $selectedTab) {
            LiveMetricsView(metricsViewModel: metricsViewModel)
                .tabItem {
                    Label("live", systemImage: "arrow.up.circle")
                }
                .tag(0)

            HistoricalReportView(metricsViewModel: metricsViewModel)
                .tabItem {
                    Label("historical", systemImage: "clock")
                }
                .tag(1)

            AgentsView(api: agentsViewModelHolder.api)
                .tabItem {
                    Label("agents", systemImage: "person.2")
                }
                .tag(2)
        }
        .frame(width: 500, height: 400)
    }
}

/// Holds the APIClient for the agents tab so it survives view re-renders.
/// Built once when the dashboard window opens; reads daemon URL + token from
/// MenubarConfigService (same pattern as SettingsWindow's MCPViewModel).
@MainActor
final class AgentsViewModelHolder: ObservableObject {
    let api: APIClient
    init() {
        let config = MenubarConfigService()
        self.api = APIClient(
            baseURL: config.daemonBaseURL,
            apiToken: config.apiToken
        )
    }
}

@MainActor
struct HistoricalReportView: View {
    @ObservedObject var metricsViewModel: MetricsViewModel

    var body: some View {
        VStack(spacing: 16) {
            HStack {
                Text("historical reports")
                    .font(.headline)
                Spacer()
            }

            HStack(spacing: 12) {
                DatePicker("from", selection: Binding(
                    get: { metricsViewModel.fromDate },
                    set: { metricsViewModel.fromDate = $0 }
                ), displayedComponents: [.date, .hourAndMinute])
                DatePicker("to", selection: Binding(
                    get: { metricsViewModel.toDate },
                    set: { metricsViewModel.toDate = $0 }
                ), displayedComponents: [.date, .hourAndMinute])

                Picker("resolution", selection: Binding(
                    get: { metricsViewModel.resolution },
                    set: { metricsViewModel.resolution = $0 }
                )) {
                    Text("hour").tag("hour")
                    Text("day").tag("day")
                    Text("week").tag("week")
                }
            }

            Button("load") {
                metricsViewModel.fetchHistorical()
            }
            .disabled(metricsViewModel.isLoadingHistorical)

            if metricsViewModel.isLoadingHistorical {
                ProgressView("loading...")
                    .padding()
            } else {
                if metricsViewModel.historicalData.isEmpty {
                    Text("select date range and click load")
                        .foregroundColor(.secondary)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    List(metricsViewModel.historicalData) { point in
                        HStack {
                            Text(point.name)
                                .font(.system(size: 12, weight: .medium))
                            Spacer()
                            Text(String(format: "%.2f", point.value))
                                .font(.system(size: 12, design: .monospaced))
                                .foregroundColor(.secondary)
                            Text(point.timestamp)
                                .font(.system(size: 10))
                                .foregroundColor(.secondary)
                        }
                    }
                }
            }

            Spacer()
        }
        .padding()
    }
}

#Preview {
    DashboardWindow(metricsViewModel: MetricsViewModel(dashboardService: DashboardService()))
}
