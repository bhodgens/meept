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
    
    var body: some View {
        VStack(spacing: 12) {
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
                    onSave(config)
                }
                .keyboardShortcut("s", modifiers: .command)
                Button("reset") {
                    // Would reload from server
                }
            }
            
            TextEditor(text: $config)
                .font(.system(.body, design: .monospaced))
                .padding(8)
                .background(Color.gray.opacity(0.1))
                .cornerRadius(4)
            
            HStack {
                Image(systemName: "checkmark.circle")
                    .foregroundColor(showValidationSuccess ? .green : .clear)
                Text("edit json5 configuration")
                    .font(.caption)
                    .foregroundColor(.secondary)
                Spacer()
            }
        }
        .padding(8)
    }
}

#Preview {
    ClientConfigView(
        config: .constant("{\n  \"key\": \"value\"\n}"),
        isSaving: false,
        onSave: { _ in }
    )
}
