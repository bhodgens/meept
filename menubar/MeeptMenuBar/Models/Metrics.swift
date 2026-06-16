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
