//
//  MemoryReviewView.swift
//  MeeptMenuBar
//
//  SwiftUI view for the "memory" tab in Settings. Shows pending auto-claims,
//  pending decisions, and pending predictions from the epistemic review queue.
//  Auto-claims have promote/reject buttons; decisions and predictions are
//  display-only in v1.
//

import SwiftUI

@MainActor
struct MemoryReviewView: View {
    @ObservedObject var viewModel: MemoryReviewViewModel

    var body: some View {
        VStack(spacing: 0) {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    // MARK: - Auto-claims section
                    sectionHeader("auto-claims", count: viewModel.autoClaims.count)
                    if viewModel.autoClaims.isEmpty {
                        emptyState("no auto-claims pending review")
                    } else {
                        VStack(spacing: 4) {
                            ForEach(viewModel.autoClaims) { claim in
                                claimRow(claim)
                            }
                        }
                    }

                    Divider()

                    // MARK: - Pending decisions section
                    sectionHeader("pending decisions", count: viewModel.pendingDecisions.count)
                    if viewModel.pendingDecisions.isEmpty {
                        emptyState("no pending decisions")
                    } else {
                        VStack(spacing: 4) {
                            ForEach(viewModel.pendingDecisions) { decision in
                                decisionRow(decision)
                            }
                        }
                    }

                    Divider()

                    // MARK: - Pending predictions section
                    sectionHeader("pending predictions", count: viewModel.pendingPredictions.count)
                    if viewModel.pendingPredictions.isEmpty {
                        emptyState("no pending predictions")
                    } else {
                        VStack(spacing: 4) {
                            ForEach(viewModel.pendingPredictions) { prediction in
                                predictionRow(prediction)
                            }
                        }
                    }
                }
                .padding(12)
            }

            Divider()

            HStack(spacing: 8) {
                if viewModel.isLoading {
                    ProgressView()
                        .controlSize(.small)
                }
                Spacer()
                Button(action: { viewModel.refresh() }) {
                    Label("refresh", systemImage: "arrow.clockwise")
                }
                .buttonStyle(.bordered)
                .disabled(viewModel.isLoading)
            }
            .padding(8)
        }
        .alert(
            "memory action failed",
            isPresented: $viewModel.showError
        ) {
            Button("ok", role: .cancel) { }
        } message: {
            Text(viewModel.errorMessage ?? "unknown error")
        }
        .onAppear {
            viewModel.startPolling()
        }
        .onDisappear {
            viewModel.stopPolling()
        }
    }

    // MARK: - Row builders

    private func claimRow(_ claim: MemoryResult) -> some View {
        HStack(alignment: .top, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(claim.memory.content)
                    .lineLimit(3)
                if let cat = claim.memory.category, !cat.isEmpty {
                    Text(cat)
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
                if let conf = claim.confidence {
                    Text(String(format: "trust: %.2f", conf))
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .monospacedDigit()
                }
            }
            Spacer()
            VStack(spacing: 4) {
                Button(action: {
                    guard !viewModel.isPending(claim) else { return }
                    Task { await viewModel.promote(claim) }
                }) {
                    Text("promote")
                        .frame(width: 64)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.small)
                .disabled(viewModel.isPending(claim))

                Button(action: {
                    guard !viewModel.isPending(claim) else { return }
                    Task { await viewModel.reject(claim) }
                }) {
                    Text("reject")
                        .frame(width: 64)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
                .disabled(viewModel.isPending(claim))
            }
        }
        .padding(8)
        .background(Color(NSColor.controlBackgroundColor))
        .cornerRadius(6)
    }

    private func decisionRow(_ decision: MemoryResult) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(decision.memory.content)
                .lineLimit(3)
            if let outcome = decision.expectedOutcome {
                Text("expected: \(outcome)")
                    .font(.caption)
                    .foregroundColor(.secondary)
                    .lineLimit(2)
            }
            if let due = decision.reviewDue {
                Text("review due: \(due)")
                    .font(.caption)
                    .foregroundColor(.orange)
            }
        }
        .padding(8)
        .background(Color(NSColor.controlBackgroundColor))
        .cornerRadius(6)
    }

    private func predictionRow(_ prediction: MemoryResult) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(prediction.memory.content)
                .lineLimit(3)
            if let horizon = prediction.horizon {
                Text("horizon: \(horizon)")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
        }
        .padding(8)
        .background(Color(NSColor.controlBackgroundColor))
        .cornerRadius(6)
    }

    // MARK: - Helpers

    private func sectionHeader(_ title: String, count: Int) -> some View {
        HStack {
            Text(title)
                .font(.headline)
            Text("(\(count))")
                .font(.caption)
                .foregroundColor(.secondary)
                .monospacedDigit()
            Spacer()
        }
    }

    private func emptyState(_ message: String) -> some View {
        Text(message)
            .font(.caption)
            .foregroundColor(.secondary)
            .italic()
            .frame(maxWidth: .infinity, alignment: .center)
            .padding(8)
    }
}

#Preview {
    MemoryReviewView(
        viewModel: MemoryReviewViewModel(
            api: APIClient()
        )
    )
}
