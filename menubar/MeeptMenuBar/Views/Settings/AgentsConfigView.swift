//
//  AgentsConfigView.swift
//  MeeptMenuBar
//

import SwiftUI

@MainActor
struct AgentsConfigView: View {
    @ObservedObject var configViewModel: ConfigViewModel

    private var selectedAgent: AgentInfo? {
        configViewModel.selectedAgent
    }

    var body: some View {
        HSplitView {
            // Left panel - Agent list
            VStack(spacing: 0) {
                HStack {
                    Text("agents")
                        .font(.headline)
                    Spacer()
                    Button(action: configViewModel.loadAgents) {
                        Image(systemName: "arrow.clockwise")
                    }
                    .buttonStyle(.borderless)
                    .help("refresh")
                }
                .padding(8)

                Divider()

                List(configViewModel.agents, selection: Binding(
                    get: { configViewModel.selectedAgentId },
                    set: { configViewModel.selectedAgentId = $0 }
                )) { agent in
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
                if let agent = configViewModel.agentDetails {
                    HStack {
                        Text(agent.name)
                            .font(.headline)
                        Spacer()
                        Toggle("enabled", isOn: Binding(
                            get: { agent.enabled },
                            set: { newValue in
                                var updated = agent
                                updated.enabled = newValue
                                configViewModel.agentDetails = updated
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
                                        configViewModel.agentDetails = updated
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
                                        configViewModel.agentDetails = updated
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
                                        configViewModel.agentDetails = updated
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
                            if let id = configViewModel.selectedAgentId {
                                configViewModel.loadAgentDetails(id)
                            }
                        }
                        .keyboardShortcut(.escape, modifiers: [])
                        Button("save") {
                            configViewModel.saveAgent()
                        }
                        .buttonStyle(.borderedProminent)
                        .disabled(configViewModel.agentDetails?.name.isEmpty ?? true)
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
        .onAppear(perform: configViewModel.loadAgents)
        .onChange(of: configViewModel.selectedAgentId) { newId in
            if let id = newId {
                configViewModel.loadAgentDetails(id)
            }
        }
    }
}

#Preview {
    AgentsConfigView(configViewModel: ConfigViewModel(configService: ConfigService()))
}
