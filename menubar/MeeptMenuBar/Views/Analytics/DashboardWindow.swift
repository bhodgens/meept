//
//  DashboardWindow.swift
//  MeeptMenuBar
//

import SwiftUI

struct DashboardWindow: View {
    @State private var selectedTab = 0
    @ObservedObject var metricsViewModel: MetricsViewModel

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
        }
        .frame(width: 500, height: 400)
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
                Text("select date range and click load")
                    .foregroundColor(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }

            Spacer()
        }
        .padding()
    }
}

#Preview {
    DashboardWindow(metricsViewModel: MetricsViewModel(dashboardService: DashboardService()))
}
