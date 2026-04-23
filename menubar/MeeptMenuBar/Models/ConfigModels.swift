//
//  ConfigModels.swift
//  MeeptMenuBar
//

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

// Helper for decoding mixed-type dictionaries
enum AnyCodableError: Error {
    case invalidValue
}

struct AnyCodable: Codable {
    let value: Any

    init(_ value: Any) {
        self.value = value
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()

        if container.decodeNil() {
            value = NSNull()
        } else if let bool = try? container.decode(Bool.self) {
            value = bool
        } else if let int = try? container.decode(Int.self) {
            value = int
        } else if let double = try? container.decode(Double.self) {
            value = double
        } else if let string = try? container.decode(String.self) {
            value = string
        } else if let array = try? container.decode([AnyCodable].self) {
            value = array.map { $0.value }
        } else if let object = try? container.decode([String: AnyCodable].self) {
            value = object.mapValues { $0.value }
        } else {
            throw AnyCodableError.invalidValue
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()

        switch value {
        case is NSNull:
            try container.encodeNil()
        case let bool as Bool:
            try container.encode(bool)
        case let int as Int:
            try container.encode(int)
        case let double as Double:
            try container.encode(double)
        case let string as String:
            try container.encode(string)
        case let array as [Any]:
            let anyCodableArray = array.map { AnyCodable($0) }
            try container.encode(anyCodableArray)
        case let object as [String: Any]:
            let anyCodableObject = object.mapValues { AnyCodable($0) }
            try container.encode(anyCodableObject)
        default:
            throw AnyCodableError.invalidValue
        }
    }
}
