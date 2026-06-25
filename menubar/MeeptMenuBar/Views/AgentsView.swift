//
//  AgentsView.swift
//  MeeptMenuBar
//
//  SwiftUI view for browsing AI employees (Phase 9 of the AI Employee Design
//  spec, docs/superpowers/specs/2026-06-23-ai-employee-design.md §"Flutter").
//  Renders a List of cards summarizing each employee with tap-through to a
//  detail sheet showing the constitution, active goals, and approve/reject
//  controls for pending plans.
//
//  All UI text is lowercase per CLAUDE.md UI convention.
//

import SwiftUI
import os.log

@MainActor
struct AgentsView: View {
    @StateObject private var viewModel: AgentsViewModel

    init(api: APIClient) {
        _viewModel = StateObject(wrappedValue: AgentsViewModel(api: api))
    }

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("agents")
                    .font(.headline)
                Spacer()
                Button(action: viewModel.refresh) {
                    Image(systemName: "arrow.clockwise")
                }
                .buttonStyle(.borderless)
                .help("refresh")
            }
            .padding(8)

            Divider()

            if viewModel.isLoading && viewModel.agents.isEmpty {
                ProgressView("loading...")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let err = viewModel.errorMessage, viewModel.agents.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: "exclamationmark.triangle")
                        .foregroundColor(.orange)
                    Text(err)
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .multilineTextAlignment(.center)
                    Button("retry", action: viewModel.refresh)
                        .buttonStyle(.bordered)
                }
                .padding()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if viewModel.agents.isEmpty {
                Text("no agents configured")
                    .foregroundColor(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                List(viewModel.agents) { agent in
                    AgentCard(agent: agent,
                              isPending: viewModel.isPending(agent.id),
                              onPause: { Task { await viewModel.pause(id: agent.id) } },
                              onResume: { Task { await viewModel.resume(id: agent.id) } })
                        .contentShape(Rectangle())
                        .onTapGesture { viewModel.select(agent) }
                }
                .listStyle(.inset)
            }
        }
        .sheet(item: $viewModel.selectedAgent) { agent in
            AgentDetailSheet(
                agent: agent,
                api: viewModel.api,
                isPending: viewModel.isPending(agent.id),
                onPause: { Task { await viewModel.pause(id: agent.id) } },
                onResume: { Task { await viewModel.resume(id: agent.id) } },
                onTrigger: { Task { await viewModel.trigger(id: agent.id) } }
            )
        }
        .onAppear {
            viewModel.startPolling()
        }
        .onDisappear {
            viewModel.stopPolling()
        }
    }
}

// MARK: - Card

struct AgentCard: View {
    let agent: Employee
    let isPending: Bool
    let onPause: () -> Void
    let onResume: () -> Void

    private var healthColor: Color {
        switch agent.health {
        case "healthy": return .green
        case "at_risk": return .yellow
        case "broken": return .red
        default: return .gray
        }
    }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            // Health dot
            Circle()
                .fill(healthColor)
                .frame(width: 10, height: 10)
                .padding(.top, 4)

            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text(agent.name)
                        .font(.system(size: 13, weight: .medium))
                    Spacer()
                    Text(agent.tierLabel)
                        .font(.system(size: 10, design: .monospaced))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(Color.blue.opacity(0.15))
                        .cornerRadius(4)
                }

                if !agent.role.isEmpty {
                    Text(agent.role)
                        .font(.system(size: 11))
                        .foregroundColor(.secondary)
                        .lineLimit(1)
                }

                HStack(spacing: 12) {
                    Label(String(format: "%.2f", agent.driftScore), systemImage: "waveform.path.ecg")
                        .font(.system(size: 10))
                        .foregroundColor(.secondary)

                    Label(agent.formattedDailyCost, systemImage: "dollarsign.circle")
                        .font(.system(size: 10))
                        .foregroundColor(.secondary)

                    Label("\(agent.findingsCount)", systemImage: "checkmark.shield")
                        .font(.system(size: 10))
                        .foregroundColor(agent.findingsCount > 0 ? .orange : .secondary)

                    Spacer()

                    if agent.status == "running" {
                        Button("pause", action: onPause)
                            .buttonStyle(.borderless)
                            .font(.system(size: 10))
                            .disabled(isPending)
                    } else if agent.status == "paused" {
                        Button("resume", action: onResume)
                            .buttonStyle(.borderless)
                            .font(.system(size: 10))
                            .disabled(isPending)
                    }
                }
            }
        }
        .padding(.vertical, 2)
    }
}

// MARK: - Detail sheet

struct AgentDetailSheet: View {
    let agent: Employee
    let api: APIClient
    let isPending: Bool
    let onPause: () -> Void
    let onResume: () -> Void
    let onTrigger: () -> Void
    @Environment(\.dismiss) private var dismiss

    @State private var goals: [Goal] = []
    @State private var goalsError: String?
    @State private var pendingGoal: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                VStack(alignment: .leading) {
                    Text(agent.name)
                        .font(.headline)
                    Text(agent.id)
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundColor(.secondary)
                }
                Spacer()
                Text(agent.tierLabel)
                    .font(.system(size: 11, design: .monospaced))
                    .padding(.horizontal, 8)
                    .padding(.vertical, 3)
                    .background(Color.blue.opacity(0.15))
                    .cornerRadius(4)
            }

            Divider()

            // Constitution summary
            VStack(alignment: .leading, spacing: 4) {
                Text("purpose")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundColor(.secondary)
                Text(agent.purpose.isEmpty ? "no purpose set" : agent.purpose)
                    .font(.system(size: 12))
            }

            if !agent.charter.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    Text("charter")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundColor(.secondary)
                    Text(agent.charter)
                        .font(.system(size: 12))
                        .lineLimit(5)
                }
            }

            if !agent.escalatesTo.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    Text("escalates to")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundColor(.secondary)
                    Text(agent.escalatesTo.joined(separator: ", "))
                        .font(.system(size: 12))
                }
            }

            // Goals section
            VStack(alignment: .leading, spacing: 4) {
                Text("goals")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundColor(.secondary)
                if let err = goalsError {
                    Text(err)
                        .font(.system(size: 11))
                        .foregroundColor(.red)
                } else if goals.isEmpty {
                    Text("no active goals")
                        .font(.system(size: 11))
                        .foregroundColor(.secondary)
                } else {
                    ForEach(goals) { goal in
                        GoalRow(
                            goal: goal,
                            isPending: pendingGoal == goal.id,
                            onApprove: {
                                guard !goal.activePlanID.isEmpty else { return }
                                pendingGoal = goal.id
                                Task {
                                    do {
                                        try await api.approvePlan(
                                            employeeID: agent.id,
                                            goalID: goal.id,
                                            planID: goal.activePlanID
                                        )
                                        await loadGoals()
                                    } catch {
                                        goalsError = error.localizedDescription
                                    }
                                    pendingGoal = nil
                                }
                            },
                            onReject: {
                                guard !goal.activePlanID.isEmpty else { return }
                                pendingGoal = goal.id
                                Task {
                                    do {
                                        try await api.rejectPlan(
                                            employeeID: agent.id,
                                            goalID: goal.id,
                                            planID: goal.activePlanID,
                                            reason: "rejected via menubar"
                                        )
                                        await loadGoals()
                                    } catch {
                                        goalsError = error.localizedDescription
                                    }
                                    pendingGoal = nil
                                }
                            }
                        )
                    }
                }
            }

            Spacer()

            // Action row
            HStack(spacing: 12) {
                if agent.status == "running" {
                    Button("pause", action: onPause)
                        .disabled(isPending)
                } else if agent.status == "paused" {
                    Button("resume", action: onResume)
                        .disabled(isPending)
                }
                Button("trigger", action: onTrigger)
                    .disabled(isPending)
                Spacer()
                Button("close") { dismiss() }
                    .buttonStyle(.borderedProminent)
            }
        }
        .padding()
        .frame(width: 420, height: 520)
        .task {
            await loadGoals()
        }
    }

    private func loadGoals() async {
        do {
            goals = try await api.listGoals(employeeID: agent.id)
            goalsError = nil
        } catch {
            goalsError = error.localizedDescription
        }
    }
}

// MARK: - View model

@MainActor
class AgentsViewModel: ObservableObject {
    @Published var agents: [Employee] = []
    @Published var isLoading = false
    @Published var errorMessage: String?
    @Published var selectedAgent: Employee?

    let api: APIClient
    private var pollTimer: Timer?
    private let updateInterval: TimeInterval = 5.0
    private var pendingIDs: Set<String> = []
    private let logger = Logger(subsystem: "com.caimlas.meept.menubar", category: "AgentsViewModel")

    init(api: APIClient) {
        self.api = api
    }

    func isPending(_ id: String) -> Bool {
        pendingIDs.contains(id)
    }

    func select(_ agent: Employee) {
        selectedAgent = agent
    }

    // MARK: - Polling

    func startPolling() {
        refresh()
        pollTimer?.invalidate()
        let timer = Timer(timeInterval: updateInterval, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.refresh()
            }
        }
        RunLoop.main.add(timer, forMode: .common)
        pollTimer = timer
    }

    func stopPolling() {
        pollTimer?.invalidate()
        pollTimer = nil
    }

    func refresh() {
        guard !isLoading else { return }
        isLoading = true
        Task { [weak self] in
            guard let self else { return }
            do {
                let result = try await api.listAgents()
                agents = result
                errorMessage = nil
            } catch {
                logger.error("failed to fetch agents: \(error.localizedDescription)")
                errorMessage = error.localizedDescription
            }
            isLoading = false
        }
    }

    // MARK: - Actions

    func pause(id: String) async {
        pendingIDs.insert(id)
        defer { pendingIDs.remove(id) }
        do {
            try await api.pauseAgent(id: id)
            refresh()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func resume(id: String) async {
        pendingIDs.insert(id)
        defer { pendingIDs.remove(id) }
        do {
            try await api.resumeAgent(id: id)
            refresh()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func trigger(id: String) async {
        pendingIDs.insert(id)
        defer { pendingIDs.remove(id) }
        do {
            try await api.triggerAgent(id: id)
            refresh()
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}

// MARK: - Goal row

struct GoalRow: View {
    let goal: Goal
    let isPending: Bool
    let onApprove: () -> Void
    let onReject: () -> Void

    private var healthColor: Color {
        switch goal.health {
        case "healthy": return .green
        case "at_risk": return .yellow
        case "broken": return .red
        default: return .gray
        }
    }

    var body: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(healthColor)
                .frame(width: 8, height: 8)

            Text(goal.title.isEmpty ? goal.id : goal.title)
                .font(.system(size: 11))
                .lineLimit(1)

            Spacer()

            if !goal.activePlanID.isEmpty {
                Button("approve", action: onApprove)
                    .buttonStyle(.borderless)
                    .font(.system(size: 10))
                    .disabled(isPending)

                Button("reject", action: onReject)
                    .buttonStyle(.borderless)
                    .font(.system(size: 10))
                    .foregroundColor(.red)
                    .disabled(isPending)
            }
        }
        .padding(.vertical, 2)
    }
}
