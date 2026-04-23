//
//  AgentsConfigView.swift
//  MeeptMenuBar
//

import SwiftUI

struct AgentsConfigView: View {
    @State private var agents: [AgentInfo] = []
    @State private var selectedAgentId: String?
    @State private var agentDetails: Agent?
    @State private var isLoading = true
    @State private var showRefresh = false
    @State private var errorMessage: String?

    private let configService = ConfigService()

    private var selectedAgent: AgentInfo? {
        agents.first { $0.id == selectedAgentId }
    }

    var body: some View {
        HSplitView {
            // Left panel - Agent list
            VStack(spacing: 0) {
                HStack {
                    Text("agents")
                        .font(.headline)
                    Spacer()
                    Button(action: loadAgents) {
                        Image(systemName: "arrow.clockwise")
                    }
                    .buttonStyle(.borderless)
                    .help("refresh")
                }
                .padding(8)

                Divider()

                List(agents, selection: $selectedAgentId) { agent in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(agent.name)
                            .font(.system(size: 13))
                        if !agent.description.isEmpty {
                            Text(agent.description)
                                .font(.system(size: 11))
                                .foregroundColor(.secondary)
                                .lineLimit(1)
                        }
                    }
                    .tag(agent.id)
                }
                .listStyle(.sidebar)
            }
            .frame(minWidth: 200)

            // Right panel - Agent details editor
            VStack(spacing: 0) {
                if let agent = agentDetails {
                    HStack {
                        Text(agent.name)
                            .font(.headline)
                        Spacer()
                        Toggle("enabled", isOn: Binding(
                            get: { agent.enabled },
                            set: { newValue in
                                var updated = agent
                                updated.enabled = newValue
                                agentDetails = updated
                            }
                        ))
                        .toggleStyle(.switch)
                    }
                    .padding(8)

                    Divider()

                    ScrollView {
                        VStack(alignment: .leading, spacing: 12) {
                            // ID (read-only)
                            VStack(alignment: .leading, spacing: 4) {
                                Text("id")
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                                Text(agent.id)
                                    .font(.system(size: NSFont.systemFontSize, weight: .regular, design: .default))
                                    .padding(6)
                                    .background(Color(NSColor.controlBackgroundColor))
                                    .cornerRadius(4)
                            }

                            // Name
                            VStack(alignment: .leading, spacing: 4) {
                                Text("name")
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                                TextField("name", text: Binding(
                                    get: { agent.name },
                                    set: {
                                        var updated = agent
                                        updated.name = $0
                                        agentDetails = updated
                                    }
                                ))
                                .textFieldStyle(.roundedBorder)
                            }

                            // Description
                            VStack(alignment: .leading, spacing: 4) {
                                Text("description")
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                                TextField("description", text: Binding(
                                    get: { agent.description },
                                    set: {
                                        var updated = agent
                                        updated.description = $0
                                        agentDetails = updated
                                    }
                                ))
                                .textFieldStyle(.roundedBorder)
                            }

                            // Prompt (markdown editor)
                            VStack(alignment: .leading, spacing: 4) {
                                Text("prompt")
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                                TextEditor(text: Binding(
                                    get: { agent.prompt },
                                    set: {
                                        var updated = agent
                                        updated.prompt = $0
                                        agentDetails = updated
                                    }
                                ))
                                .font(.system(size: NSFont.systemFontSize, weight: .regular, design: .default))
                                .frame(minHeight: 200)
                                .padding(6)
                                .background(Color(NSColor.controlBackgroundColor))
                                .cornerRadius(4)
                            }
                        }
                        .padding()
                    }

                    Divider()

                    HStack {
                        Spacer()
                        Button("cancel") {
                            if let id = selectedAgentId {
                                loadAgentDetails(id)
                            }
                        }
                        .keyboardShortcut(.escape, modifiers: [])
                        Button("save") {
                            saveAgent()
                        }
                        .buttonStyle(.borderedProminent)
                        .disabled(agentDetails?.name.isEmpty ?? true)
                    }
                    .padding(8)
                } else {
                    VStack(spacing: 16) {
                        Image(systemName: "person.crop.circle")
                            .font(.system(size: 48))
                            .foregroundColor(.secondary)
                        Text("select an agent to view details")
                            .foregroundColor(.secondary)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .frame(minWidth: 350)
        }
        .onAppear(perform: loadAgents)
        .onChange(of: selectedAgentId) { newId in
            if let id = newId {
                loadAgentDetails(id)
            }
        }
    }

    private func loadAgents() {
        isLoading = true
        configService.getAgentsList { result in
            DispatchQueue.main.async {
                isLoading = false
                switch result {
                case .success(let agentList):
                    self.agents = agentList
                case .failure(let error):
                    self.errorMessage = error.localizedDescription
                }
            }
        }
    }

    private func loadAgentDetails(_ agentId: String) {
        guard let agent = agents.first(where: { $0.id == agentId }) else {
            agentDetails = nil
            return
        }

        configService.getAgent(id: agent.id) { result in
            DispatchQueue.main.async {
                switch result {
                case .success(let details):
                    self.agentDetails = details
                case .failure:
                    // Create a basic agent from info
                    self.agentDetails = Agent(
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
    }

    private func saveAgent() {
        guard let details = agentDetails else { return }

        configService.saveAgent(id: details.id, agent: details) { result in
            DispatchQueue.main.async {
                switch result {
                case .success:
                    // Refresh the list
                    loadAgents()
                case .failure(let error):
                    self.errorMessage = error.localizedDescription
                }
            }
        }
    }
}

#Preview {
    AgentsConfigView()
}
