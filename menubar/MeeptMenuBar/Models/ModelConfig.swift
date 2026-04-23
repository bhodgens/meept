//
//  ModelConfig.swift
//  MeeptMenuBar
//

import Foundation

struct ModelsConfig: Codable {
    var model: String
    var smallModel: String
    var classifierModel: String?
    var disabledProviders: [String]
    var defaultTimeout: Int
    var modelAliases: [String: ModelAlias]?
    var providers: [String: Provider]

    struct ModelAlias: Codable {
        var models: [String]
        var timeout: Int
        var maxFails: Int
    }

    struct Provider: Codable {
        var api: String
        var options: ProviderOptions
        var models: [String: ModelDefinition]
    }

    struct ProviderOptions: Codable {
        var baseURL: String?
        var apiKey: String?
        var timeout: Int?
    }

    struct ModelDefinition: Codable {
        var name: String
        var capabilities: [String]
        var inputCost: Double
        var outputCost: Double
        var contextLimit: Int
        var maxOutput: Int
        var temperature: Double
        var topP: Double?
        var preset: String?
    }
}

// Use the PresetParams from Presets.swift but with camelCase for SwiftUI bindings
struct ModelPreset: Identifiable, Codable, Hashable {
    let id: String
    var label: String
    var description: String
    var params: ModelPresetParams
}

struct ModelPresetParams: Codable, Hashable {
    var temperature: Double?
    var topP: Double?
    var frequencyPenalty: Double?
    var presencePenalty: Double?
    var maxTokens: Int?
}

extension ModelsConfig {
    static let `default` = ModelsConfig(
        model: "anthropic/claude-sonnet-4-5",
        smallModel: "anthropic/claude-haiku-4-5",
        classifierModel: nil,
        disabledProviders: [],
        defaultTimeout: 3000,
        modelAliases: nil,
        providers: [:]
    )
}
