//
//  Agent.swift
//  MeeptMenuBar
//
//  Wire model for AI employees (Phase 9 of the AI Employee Design spec).
//  Mirrors the daemon's `agents.list` envelope (`{agents: [...]}`) and the
//  per-employee fields surfaced via `agents.get`. Lowercase JSON keys match
//  the Go side exactly so Codable round-trips without custom mapping.
//
//  NOTE: ConfigModels.swift already declares `Agent` and
//  `AgentsListResponse` types for the config editor tab. To avoid type
//  ambiguity we name our types `Employee*` here.
//

import Foundation

// MARK: - Wire types

/// Wrapper for the `GET /api/v1/agents` response: `{"agents": [...]}`.
struct EmployeeListResponse: Codable {
    let agents: [EmployeeWire]
}

/// Single entry returned by the daemon's agents.list endpoint. Combines the
/// Employee wrapper with runtime state (drift, cost, findings) produced by
/// Manager.Review. Field names mirror the Go JSON tags.
struct EmployeeWire: Codable {
    let id: String
    let name: String?
    let description: String?
    let model: String?
    let enabled: Bool?
    // Constitution-derived fields surfaced for the card view.
    let constitution: ConstitutionWire?

    // Encoding for the embedded bot.BotDefinition fields we don't decode yet.
    // Keeps Codable forward-compatible when the daemon adds new fields.
    let tools: [String]?
}

/// Constitution subset rendered in the agent detail view.
struct ConstitutionWire: Codable {
    let purpose: String?
    let role: String?
    let charter: String?
    let autonomyTier: String?
    let escalatesTo: [String]?
    let constraints: ConstraintsWire?

    enum CodingKeys: String, CodingKey {
        case purpose, role, charter
        case autonomyTier = "autonomy_tier"
        case escalatesTo = "escalates_to"
        case constraints
    }
}

struct ConstraintsWire: Codable {
    let toolsAllowed: [String]?
    let toolsForbidden: [String]?
    let riskCeiling: String?
    let never: [String]?

    enum CodingKeys: String, CodingKey {
        case toolsAllowed = "tools_allowed"
        case toolsForbidden = "tools_forbidden"
        case riskCeiling = "risk_ceiling"
        case never
    }
}

// MARK: - Row model

/// Flattened, Identifiable row model used by AgentsView. Built from
/// EmployeeWire + runtime stats. Never decoded directly from JSON.
struct Employee: Codable, Identifiable {
    let id: String
    var name: String
    var role: String
    var tier: String
    var health: String          // "healthy" | "at_risk" | "broken" | "unknown"
    var status: String          // "running" | "paused" | "error" | "stopped"
    var driftScore: Double
    var dailyCostCents: Int
    var findingsCount: Int
    var purpose: String
    var escalatesTo: [String]
    var charter: String

    /// Builds a row model from the wire type with safe defaults for every
    /// optional field so the view never has to nil-check.
    init(from wire: EmployeeWire) {
        self.id = wire.id
        self.name = wire.name ?? wire.id
        self.role = wire.constitution?.role ?? wire.description ?? ""
        self.tier = wire.constitution?.autonomyTier ?? "tier_1_reactive"
        self.health = "unknown"
        self.status = wire.enabled == false ? "paused" : "running"
        self.driftScore = 0
        self.dailyCostCents = 0
        self.findingsCount = 0
        self.purpose = wire.constitution?.purpose ?? ""
        self.escalatesTo = wire.constitution?.escalatesTo ?? []
        self.charter = wire.constitution?.charter ?? ""
    }
}

extension Employee {
    /// Formatted daily cost in USD (e.g. "$1.23"). Returns "$0.00" when the
    /// daemon has not reported any cost.
    var formattedDailyCost: String {
        String(format: "$%d.%02d", dailyCostCents / 100, dailyCostCents % 100)
    }

    /// Short label for the tier badge (e.g. "t2 propose").
    var tierLabel: String {
        switch tier {
        case "tier_1_reactive": return "t1 reactive"
        case "tier_2_propose": return "t2 propose"
        case "tier_3_autonomous": return "t3 autonomous"
        default: return tier
        }
    }
}
