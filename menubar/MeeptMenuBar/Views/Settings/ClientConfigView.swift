//
//  ClientConfigView.swift
//  MeeptMenuBar
//

import SwiftUI

struct ClientConfigView: View {
    @Binding var config: String
    @State private var showValidationSuccess = false
    let isSaving: Bool
    let onSave: (String) -> Void
    var onAppear: (() -> Void)? = nil

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("client configuration")
                    .font(.headline)
                Spacer()
                if isSaving {
                    ProgressView()
                        .scaleEffect(0.8)
                    Text("saving...")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
                Button("save") {
                    saveSettings()
                }
                .keyboardShortcut("s", modifiers: .command)
                .buttonStyle(.borderedProminent)
            }
            .padding(8)

            Divider()

            TextEditor(text: $config)
                .font(.system(size: 12, design: .monospaced))
                .padding(8)
                .background(Color(NSColor.controlBackgroundColor))
                .cornerRadius(4)

            Text("raw json5 editor - comments preserved")
                .font(.caption)
                .foregroundColor(.secondary)
        }
        .padding(8)
        .onAppear {
            onAppear?()
        }
    }

    private func saveSettings() {
        onSave(config)
        showValidationSuccess = true

        Task {
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            showValidationSuccess = false
        }
    }
}

#Preview {
    ClientConfigView(
        config: .constant("// Loading..."),
        isSaving: false,
        onSave: { _ in }
    )
}
