//
//  ConfigViewModel.swift
//  MeeptMenuBar
//

import SwiftUI
import os.log

@MainActor
class ConfigViewModel: ObservableObject {
    // Client config
    @Published var clientConfig = "// Loading..."
    @Published var modelsConfig = "// Loading..."

    // Agents
    @Published var agents: [AgentInfo] = []
    @Published var selectedAgentId: String?
    @Published var agentDetails: Agent?

    // UI state
    @Published var isSaving = false
    @Published var isLoadingAgents = true
    @Published var showSaveError = false
    @Published var showNormalizeError = false
    @Published var errorMessage: String?

    private let configService: ConfigService
    private let logger = Logger(subsystem: "com.caimlas.meept.menubar", category: "ConfigViewModel")

    init(configService: ConfigService) {
        self.configService = configService
    }

    // MARK: - Config Loading

    func loadConfigs() {
        loadClientConfig()
        loadModelsConfig()
        loadAgents()
    }

    func loadClientConfig() {
        Task { [weak self] in
            guard let self else { return }
            do {
                clientConfig = try await configService.getClientConfig()
            } catch {
                clientConfig = "// Error loading config"
            }
        }
    }

    func loadModelsConfig() {
        Task { [weak self] in
            guard let self else { return }
            do {
                modelsConfig = try await configService.getModelsConfig()
            } catch {
                modelsConfig = "// Error loading config"
            }
        }
    }

    // MARK: - Config Saving

    func saveClientConfig(content: String) {
        isSaving = true
        Task { [weak self] in
            guard let self else { return }
            // Two distinct failure modes warrant separate UI surfaces:
            // - normalize failure: bad JSON5 syntax (showNormalizeError)
            // - save failure: server-side rejection (showSaveError)
            do {
                _ = try await configService.normalizeJSON5(content: content)
            } catch {
                self.showNormalizeError = true
                self.isSaving = false
                return
            }
            do {
                try await configService.saveClientConfig(content: content)
            } catch {
                self.showSaveError = true
            }
            self.isSaving = false
        }
    }

    func saveModelsConfig(content: String) {
        isSaving = true
        Task { [weak self] in
            guard let self else { return }
            do {
                _ = try await configService.normalizeJSON5(content: content)
            } catch {
                self.showNormalizeError = true
                self.isSaving = false
                return
            }
            do {
                try await configService.saveModelsConfig(content: content)
            } catch {
                self.showSaveError = true
            }
            self.isSaving = false
        }
    }

    // MARK: - Agents CRUD

    func loadAgents() {
        isLoadingAgents = true
        Task { [weak self] in
            guard let self else { return }
            do {
                agents = try await configService.getAgentsList()
            } catch {
                errorMessage = error.localizedDescription
            }
            isLoadingAgents = false
        }
    }

    func loadAgentDetails(_ agentId: String) {
        guard let agent = agents.first(where: { $0.id == agentId }) else {
            agentDetails = nil
            return
        }

        Task { [weak self] in
            guard let self else { return }
            do {
                agentDetails = try await configService.getAgent(id: agent.id)
            } catch {
                // Create a basic agent from info
                agentDetails = Agent(
                    id: agent.id,
                    name: agent.name,
                    description: agent.description,
                    prompt: "",
                    frontmatter: nil,
                    enabled: agent.enabled
                )
            }
        }
    }

    func saveAgent() {
        guard let details = agentDetails else { return }

        Task { [weak self] in
            guard let self else { return }
            do {
                try await configService.saveAgent(id: details.id, agent: details)
                loadAgents()
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    // MARK: - Helpers

    var selectedAgent: AgentInfo? {
        agents.first { $0.id == selectedAgentId }
    }
}
