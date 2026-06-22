//
//  SessionDesignation.swift
//  MeeptMenuBar
//

import Foundation

// MARK: - DesignationStatus

enum DesignationStatus: String, Codable, CaseIterable {
    case none
    case waitingHuman
    case humanResponded
    case botThinking
    case requiresApproval
}

// MARK: - Priority

enum Priority: String, Codable, CaseIterable, Comparable {
    case urgent
    case high
    case normal
    case low

    var orderValue: Int {
        switch self {
        case .urgent: return 4
        case .high: return 3
        case .normal: return 2
        case .low: return 1
        }
    }

    static func < (lhs: Priority, rhs: Priority) -> Bool {
        return lhs.orderValue < rhs.orderValue
    }

    var colorHex: String {
        switch self {
        case .urgent: return "FF0000"
        case .high: return "FF8800"
        case .normal: return "0088FF"
        case .low: return "888888"
        }
    }
}

// MARK: - SessionDesignation

struct SessionDesignation: Codable {
    var status: DesignationStatus
    var reason: String
    var createdAt: String
    var updatedAt: String
    var acknowledgedAt: String?
    var priority: Priority

    enum CodingKeys: String, CodingKey {
        case status
        case reason
        case createdAt = "created_at"
        case updatedAt = "updated_at"
        case acknowledgedAt = "acknowledged_at"
        case priority
    }
}

// MARK: - DesignatedSessionsResponse

struct DesignatedSessionsResponse: Codable {
    var designatedCount: Int
    var sessions: [DesignatedSessionSummary]

    enum CodingKeys: String, CodingKey {
        case designatedCount = "designated_count"
        case sessions
    }
}

struct DesignatedSessionSummary: Codable {
    var id: String
    var name: String
    var designation: SessionDesignation
    var lastActivity: String

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case designation
        case lastActivity = "last_activity"
    }
}

// MARK: - BadgeState (internal to manager)

struct BadgeState {
    var designatedCount: Int
    var urgentCount: Int
    var highCount: Int
    var hasUrgent: Bool
    var hasHighOrMore: Bool
}
