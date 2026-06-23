//
//  ReasoningConfigView.swift
//  MeeptMenuBar
//
//  LLM reasoning effort tier configuration (spec §8.6).
//

import SwiftUI

/// ReasoningTier represents a single reasoning effort level.
struct ReasoningTier: Identifiable, Hashable {
    let id = UUID()
    let name: String
    let description: String
    let defaultBudget: Int
}

/// ReasoningAgent represents an agent's reasoning configuration.
struct ReasoningAgent: Identifiable, Codable, Hashable {
    let id: String
    let agentId: String
    let hasReasoning: Bool
    let effort: String?
    let effectiveEffort: String?
}

@MainActor
struct ReasoningConfigView: View {
    let api: APIClient

    @State private var tiers: [ReasoningTier] = []
    @State private var agents: [ReasoningAgent] = []
    @State private var budgets: [String: Int] = [:]
    @State private var isLoading = false
    @State private var errorMessage: String?
    @State private var budgetInputLow = ""
    @State private var budgetInputMedium = ""
    @State private var budgetInputHigh = ""
    @State private var budgetInputXHigh = ""
    @State private var budgetInputMax = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            if isLoading {
                ProgressView("loading reasoning configuration...")
                    .frame(maxWidth: .infinity)
            } else if let errorMessage {
                Text(errorMessage)
                    .foregroundColor(.red)
                    .font(.caption)
            } else {
                // Tiers
                GroupBox("effort tiers") {
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(tiers) { tier in
                            HStack {
                                Text(tier.name)
                                    .font(.system(.body, design: .monospaced))
                                    .frame(width: 60, alignment: .leading)
                                Text(tier.description)
                                    .foregroundColor(.secondary)
                                    .font(.caption)
                                Spacer()
                                Text("\(tier.defaultBudget) tokens")
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                            }
                        }
                    }
                    .padding(8)
                }

                // Global budgets
                GroupBox("global budgets") {
                    VStack(alignment: .leading, spacing: 8) {
                        budgetRow("low", text: $budgetInputLow)
                        budgetRow("medium", text: $budgetInputMedium)
                        budgetRow("high", text: $budgetInputHigh)
                        budgetRow("xhigh", text: $budgetInputXHigh)
                        budgetRow("max", text: $budgetInputMax)

                        Button("save budgets") {
                            saveBudgets()
                        }
                        .buttonStyle(.borderedProminent)
                        .padding(.top, 4)
                    }
                    .padding(8)
                }

                // Per-agent
                if !agents.isEmpty {
                    GroupBox("per-agent effort") {
                        VStack(alignment: .leading, spacing: 4) {
                            ForEach(agents) { agent in
                                HStack {
                                    Text(agent.agentId)
                                        .font(.system(.body, design: .monospaced))
                                        .frame(width: 100, alignment: .leading)
                                    Text(agent.effectiveEffort ?? agent.effort ?? "(default)")
                                        .foregroundColor(.secondary)
                                        .font(.caption)
                                }
                            }
                        }
                        .padding(8)
                    }
                }
            }
        }
        .padding()
        .task {
            await loadAll()
        }
    }

    private func budgetRow(_ label: String, text: Binding<String>) -> some View {
        HStack {
            Text(label)
                .font(.system(.body, design: .monospaced))
                .frame(width: 60, alignment: .leading)
            TextField("tokens", text: text)
                .textFieldStyle(.roundedBorder)
                .frame(width: 100)
            Text("\(budgets[label] ?? 0)")
                .font(.caption)
                .foregroundColor(.secondary)
        }
    }

    private func loadAll() async {
        isLoading = true
        errorMessage = nil
        do {
            try await loadTiers()
            try await loadBudgets()
            try await loadAgents()
        } catch {
            errorMessage = "failed to load: \(error.localizedDescription)"
        }
        isLoading = false
    }

    private func loadTiers() async throws {
        let data = try await api.getReasoningTiers()
        struct TiersResponse: Codable {
            let tiers: [TierInfo]
        }
        struct TierInfo: Codable {
            let name: String
            let description: String
            let default_budget: Int
        }
        let resp = try JSONDecoder().decode(TiersResponse.self, from: data)
        tiers = resp.tiers.map { ReasoningTier(name: $0.name, description: $0.description, defaultBudget: $0.default_budget) }
    }

    private func loadBudgets() async throws {
        let data = try await api.getReasoningBudgets()
        struct BudgetsResponse: Codable {
            let budgets: [String: Int]
        }
        let resp = try JSONDecoder().decode(BudgetsResponse.self, from: data)
        budgets = resp.budgets
        budgetInputLow = String(budgets["low"] ?? 0)
        budgetInputMedium = String(budgets["medium"] ?? 0)
        budgetInputHigh = String(budgets["high"] ?? 0)
        budgetInputXHigh = String(budgets["xhigh"] ?? 0)
        budgetInputMax = String(budgets["max"] ?? 0)
    }

    private func loadAgents() async throws {
        let data = try await api.getReasoningAgents()
        struct AgentResponse: Codable {
            let agent_id: String
            let has_reasoning: Bool
            let config: ConfigInfo?
            let effective_effort: String?
            enum CodingKeys: String, CodingKey {
                case agent_id, has_reasoning, config, effective_effort
            }
        }
        struct ConfigInfo: Codable {
            let effort: String?
        }
        let resp = try JSONDecoder().decode([AgentResponse].self, from: data)
        agents = resp.map {
            ReasoningAgent(id: $0.agent_id, agentId: $0.agent_id, hasReasoning: $0.has_reasoning, effort: $0.config?.effort, effectiveEffort: $0.effective_effort)
        }
    }

    private func saveBudgets() {
        var body: [String: Int] = [:]
        if let v = Int(budgetInputLow), v > 0 { body["low"] = v }
        if let v = Int(budgetInputMedium), v > 0 { body["medium"] = v }
        if let v = Int(budgetInputHigh), v > 0 { body["high"] = v }
        if let v = Int(budgetInputXHigh), v > 0 { body["xhigh"] = v }
        if let v = Int(budgetInputMax), v > 0 { body["max"] = v }

        Task {
            do {
                _ = try await api.setReasoningBudgets(body)
                try await loadBudgets()
            } catch {
                errorMessage = "failed to save budgets: \(error.localizedDescription)"
            }
        }
    }
}
