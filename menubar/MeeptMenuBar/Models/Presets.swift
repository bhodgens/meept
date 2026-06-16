//
//  Presets.swift
//  MeeptMenuBar
//

import Foundation

struct PresetDefinition: Codable {
    let label: String
    let description: String
    let params: PresetParams
}

struct PresetParams: Codable {
    let temperature: Double?
    let top_p: Double?
    let frequency_penalty: Double?
    let presence_penalty: Double?
    let max_tokens: Int?
}

struct PresetsConfig: Codable {
    let presets: [String: PresetDefinition]
}

extension PresetsConfig {
    static let `default` = PresetsConfig(presets: [
        "development": PresetDefinition(
            label: "development",
            description: "balanced for coding tasks",
            params: PresetParams(temperature: 0.3, top_p: 0.9, frequency_penalty: 0.0, presence_penalty: 0.0, max_tokens: nil)
        ),
        "debugging": PresetDefinition(
            label: "debugging",
            description: "methodical troubleshooting",
            params: PresetParams(temperature: 0.2, top_p: 0.85, frequency_penalty: 0.0, presence_penalty: 0.0, max_tokens: nil)
        ),
        "planning": PresetDefinition(
            label: "planning",
            description: "structured thinking",
            params: PresetParams(temperature: 0.4, top_p: 0.9, frequency_penalty: 0.0, presence_penalty: 0.0, max_tokens: nil)
        ),
        "creative": PresetDefinition(
            label: "creative writing",
            description: "high creativity mode",
            params: PresetParams(temperature: 0.9, top_p: 0.95, frequency_penalty: 0.5, presence_penalty: 0.5, max_tokens: nil)
        ),
        "research": PresetDefinition(
            label: "research",
            description: "analytical and thorough",
            params: PresetParams(temperature: 0.5, top_p: 0.9, frequency_penalty: 0.0, presence_penalty: 0.0, max_tokens: nil)
        ),
        "fast": PresetDefinition(
            label: "fast",
            description: "quick responses",
            params: PresetParams(temperature: 0.5, top_p: 0.8, frequency_penalty: 0.0, presence_penalty: 0.0, max_tokens: nil)
        ),
        "detailed": PresetDefinition(
            label: "detailed",
            description: "comprehensive answers",
            params: PresetParams(temperature: 0.4, top_p: 0.9, frequency_penalty: 0.0, presence_penalty: 0.0, max_tokens: 4096)
        )
    ])
}
