//
//  ReviewGradesView.swift
//  MeeptMenuBar
//

import SwiftUI

// MARK: - Codable models (frozen C5/C6 response shape from GET /api/v1/metrics/reviews)

/// Response of `GET /api/v1/metrics/reviews?window=...&group_by=...`.
/// Keys are frozen by contract C5 — decode exactly, no aliasing.
struct ReviewGradesResponse: Codable {
    let window: String
    let group_by: String
    let rows: [ReviewGradeRow]
    let workflow_excluded: Int
}

struct ReviewGradeRow: Codable, Identifiable {
    let group: String
    let verdicts: Int
    let defects: Int
    let pass_rate: Double
    let label: String

    var id: String { group }
}

// MARK: - View model

/// Fetches review verdict aggregates from the daemon's metrics endpoint.
/// HTTP style mirrors DashboardService (MenubarConfigService for URL + token,
/// localhost trust delegate for the daemon's self-signed cert). Fetch
/// failures degrade to the empty state like LiveMetricsView — never a crash.
@MainActor
final class ReviewGradesViewModel: ObservableObject {
    @Published var response: ReviewGradesResponse?
    @Published var breakdown: [ReviewModelCategoryRow]?
    @Published var isLoading = false
    @Published var errorMessage: String?

    @Published var window = "7d"
    @Published var groupBy = "model"

    private let config = MenubarConfigService()
    private lazy var service = DashboardService()
    private lazy var session: URLSession = {
        let urlConfig = URLSessionConfiguration.ephemeral
        urlConfig.tlsMinimumSupportedProtocolVersion = .TLSv12
        return URLSession(
            configuration: urlConfig,
            delegate: LocalhostTrustDelegate.shared,
            delegateQueue: nil
        )
    }()

    func fetch() {
        isLoading = true
        Task { [weak self] in
            guard let self else { return }
            do {
                response = try await fetchReviewGrades(window: window, groupBy: groupBy)
                errorMessage = nil
            } catch {
                // Degrade to the previous response / empty state; surface the
                // error the same way LiveMetricsView does.
                errorMessage = error.localizedDescription
            }
            await fetchBreakdown()
            isLoading = false
        }
    }

    /// Best-effort: on failure keeps the previous breakdown so the view is
    /// never blanked by a degraded fetch.
    private func fetchBreakdown() async {
        do {
            breakdown = try await service.getReviewModelCategoryBreakdown(window: window)
        } catch {
            NSLog("ReviewGradesViewModel: breakdown fetch failed: \(error.localizedDescription)")
        }
    }

    private func fetchReviewGrades(window: String, groupBy: String) async throws -> ReviewGradesResponse {
        var components = URLComponents(
            url: baseURL.appendingPathComponent("/api/v1/metrics/reviews"),
            resolvingAgainstBaseURL: true
        )
        components?.queryItems = [
            URLQueryItem(name: "window", value: window),
            URLQueryItem(name: "group_by", value: groupBy)
        ]
        guard let url = components?.url else {
            throw APIError.invalidURL
        }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        if let token = config.apiToken, !token.isEmpty {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        let (data, urlResponse) = try await session.data(for: request)
        if let httpResponse = urlResponse as? HTTPURLResponse,
           !(200..<300).contains(httpResponse.statusCode) {
            throw APIError.httpError(httpResponse.statusCode, nil)
        }
        return try JSONDecoder().decode(ReviewGradesResponse.self, from: data)
    }

    private var baseURL: URL {
        URL(string: config.daemonBaseURL) ?? URL(string: "https://localhost:8081")!
    }
}

// MARK: - View

struct ReviewGradesView: View {
    @StateObject private var viewModel = ReviewGradesViewModel()

    var body: some View {
        VStack(spacing: 16) {
            HStack {
                Text("review grades")
                    .font(.headline)
                Spacer()
                if viewModel.isLoading {
                    ProgressView()
                        .scaleEffect(0.8)
                }
            }

            HStack(spacing: 12) {
                Picker("window", selection: $viewModel.window) {
                    Text("24h").tag("24h")
                    Text("7d").tag("7d")
                    Text("30d").tag("30d")
                    Text("all").tag("all")
                }
                .frame(width: 110)

                Picker("group by", selection: $viewModel.groupBy) {
                    Text("model").tag("model")
                    Text("judge").tag("judge")
                }
                .frame(width: 130)

                Button("load") {
                    viewModel.fetch()
                }
                .disabled(viewModel.isLoading)
            }

            if let errorMessage = viewModel.errorMessage {
                Text(errorMessage)
                    .font(.caption)
                    .foregroundColor(.red)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }

            breakdownSection(viewModel.breakdown)

            if let response = viewModel.response {
                if response.rows.isEmpty {
                    Text("no review grades available")
                        .foregroundColor(.secondary)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    VStack(spacing: 0) {
                        gradeHeaderRow
                        Divider()
                        List(response.rows) { row in
                            gradeRow(row)
                        }
                        .listStyle(.plain)
                    }
                    .cornerRadius(6)

                    if response.workflow_excluded > 0 {
                        Text("workflow-scope verdicts excluded: \(response.workflow_excluded)")
                            .font(.caption)
                            .foregroundColor(.secondary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            } else if viewModel.errorMessage == nil {
                Text("no review grades available")
                    .foregroundColor(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }

            Spacer()
        }
        .padding()
        .onAppear {
            viewModel.fetch()
        }
        .onChange(of: viewModel.window) { _ in
            viewModel.fetch()
        }
        .onChange(of: viewModel.groupBy) { _ in
            viewModel.fetch()
        }
    }

    // MARK: - model x category breakdown

    private func breakdownSection(_ rows: [ReviewModelCategoryRow]?) -> some View {
        VStack(spacing: 4) {
            Text("model x category breakdown")
                .font(.system(size: 11, weight: .semibold))
                .foregroundColor(.secondary)
                .frame(maxWidth: .infinity, alignment: .leading)

            if let rows, !rows.isEmpty {
                VStack(spacing: 0) {
                    breakdownHeaderRow
                    Divider()
                    ForEach(rows) { row in
                        breakdownRow(row)
                    }
                }
                .cornerRadius(6)
            } else {
                Text("no breakdown available")
                    .font(.caption)
                    .foregroundColor(.secondary)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding(.vertical, 6)
            }
        }
    }

    private var breakdownHeaderRow: some View {
        HStack {
            Text("model")
                .font(.system(size: 11, weight: .semibold))
                .frame(maxWidth: .infinity, alignment: .leading)
            Text("category")
                .font(.system(size: 11, weight: .semibold))
                .frame(width: 110, alignment: .leading)
            Text("count")
                .font(.system(size: 11, weight: .semibold))
                .frame(width: 50, alignment: .trailing)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(Color.secondary.opacity(0.08))
    }

    private func breakdownRow(_ row: ReviewModelCategoryRow) -> some View {
        HStack {
            Text(row.model.isEmpty ? "unknown" : row.model)
                .font(.system(size: 12))
                .lineLimit(1)
                .truncationMode(.middle)
                .frame(maxWidth: .infinity, alignment: .leading)
            Text(row.category.isEmpty ? "unknown" : row.category)
                .font(.system(size: 12))
                .foregroundColor(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)
                .frame(width: 110, alignment: .leading)
            Text("\(row.count)")
                .font(.system(size: 12, design: .monospaced))
                .frame(width: 50, alignment: .trailing)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
    }

    private var gradeHeaderRow: some View {
        HStack {
            Text("group")
                .font(.system(size: 11, weight: .semibold))
                .frame(maxWidth: .infinity, alignment: .leading)
            Text("verdicts")
                .font(.system(size: 11, weight: .semibold))
                .frame(width: 60, alignment: .trailing)
            Text("defects")
                .font(.system(size: 11, weight: .semibold))
                .frame(width: 60, alignment: .trailing)
            Text("pass rate")
                .font(.system(size: 11, weight: .semibold))
                .frame(width: 70, alignment: .trailing)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 6)
        .background(Color.secondary.opacity(0.08))
    }

    private func gradeRow(_ row: ReviewGradeRow) -> some View {
        HStack {
            Text(row.label.isEmpty ? row.group : row.label)
                .font(.system(size: 12, weight: .medium))
                .lineLimit(1)
                .truncationMode(.middle)
                .frame(maxWidth: .infinity, alignment: .leading)
            Text("\(row.verdicts)")
                .font(.system(size: 12, design: .monospaced))
                .frame(width: 60, alignment: .trailing)
            Text("\(row.defects)")
                .font(.system(size: 12, design: .monospaced))
                .foregroundColor(row.defects > 0 ? .orange : .secondary)
                .frame(width: 60, alignment: .trailing)
            Text(String(format: "%.0f%%", row.pass_rate * 100))
                .font(.system(size: 12, design: .monospaced))
                .foregroundColor(.secondary)
                .frame(width: 70, alignment: .trailing)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
    }
}

#Preview {
    ReviewGradesView()
}
