//
//  DashboardWindow.swift
//  MeeptMenuBar
//

import SwiftUI

struct DashboardWindow: View {
    @State private var selectedTab = 0
    
    var body: some View {
        TabView(selection: $selectedTab) {
            LiveMetricsView()
                .tabItem {
                    Label("live", systemImage: "arrow.up.circle")
                }
                .tag(0)
            
            HistoricalReportView()
                .tabItem {
                    Label("historical", systemImage: "clock")
                }
                .tag(1)
        }
        .frame(width: 500, height: 400)
    }
}

struct HistoricalReportView: View {
    @State private var fromDate = Date().addingTimeInterval(-3600 * 24) // 24 hours ago
    @State private var toDate = Date()
    @State private var resolution = "hour"
    @State private var isLoading = false
    
    var body: some View {
        VStack(spacing: 16) {
            HStack {
                Text("historical reports")
                    .font(.headline)
                Spacer()
            }
            
            HStack(spacing: 12) {
                DatePicker("from", selection: $fromDate, displayedComponents: [.date, .hourAndMinute])
                DatePicker("to", selection: $toDate, displayedComponents: [.date, .hourAndMinute])
                
                Picker("resolution", selection: $resolution) {
                    Text("hour").tag("hour")
                    Text("day").tag("day")
                    Text("week").tag("week")
                }
            }
            
            Button("load") {
                isLoading = true
                // Would call dashboardService.getHistoricalMetrics
                DispatchQueue.main.asyncAfter(deadline: .now() + 1) {
                    isLoading = false
                }
            }
            .disabled(isLoading)
            
            if isLoading {
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
    DashboardWindow()
}
