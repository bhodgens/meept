//
//  ConfigModels.swift
//  MeeptMenuBar
//

import AnyCodable
import Foundation

// MARK: - Agent Models

struct AgentInfo: Codable, Identifiable, Hashable, Equatable {
    let id: String
    let name: String
    let description: String
    let enabled: Bool
}

struct AgentsListResponse: Codable {
    let agents: [AgentInfo]
    let count: Int
}

struct Agent: Codable {
    var id: String
    var name: String
    var description: String
    var prompt: String
    var frontmatter: [String: AnyCodable]?
    var enabled: Bool
}

