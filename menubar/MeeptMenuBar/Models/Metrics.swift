//
//  Metrics.swift
//  MeeptMenuBar
//

import Foundation

struct LiveMetrics: Codable {
    let timestamp: String
    let active_agents: Int
    let requests_per_sec: Double
    let token_usage_rate: Double
    let queue_depth: Int
    let model_failovers: Int
}

struct MetricPoint: Codable, Identifiable {
    let timestamp: String
    let name: String
    let value: Double
    let tags: [String: String]?

    var id: String { "\(name)-\(timestamp)" }
}

// MARK: - model x category review breakdown (GET /api/v1/metrics/reviews?group_by=model_category)

/// One row of the `breakdown` array. JSON keys are contract — decode verbatim.
struct ReviewModelCategoryRow: Codable, Identifiable {
    let model: String
    let category: String
    let count: Int

    var id: String { "\(model)-\(category)" }
}

/// Minimal decode envelope for the model_category group_by mode; the
/// `breakdown` array is always present in that mode.
struct ReviewModelCategoryResponse: Codable {
    let breakdown: [ReviewModelCategoryRow]
}
