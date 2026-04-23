//
//  ClientConfigView.swift
//  MeeptMenuBar
//

import SwiftUI

struct ClientConfigView: View {
    @Binding var config: String
    @State private var settings: ClientSettings = .default
    @State private var showRawEditor = false
    @State private var showValidationSuccess = false
    let isSaving: Bool
    let onSave: (String) -> Void

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("client configuration")
                    .font(.headline)
                Spacer()
                Button("raw json") {
                    showRawEditor.toggle()
                }
                .buttonStyle(.borderless)
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

            if showRawEditor {
                rawEditorView
            } else {
                formView
            }
        }
        .padding(8)
        .onAppear {
            parseSettings()
        }
    }

    private var formView: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                // General Settings Section
                GroupBox("general") {
                    VStack(alignment: .leading, spacing: 12) {
                        LabeledContent("theme") {
                            Picker("theme", selection: $settings.theme) {
                                Text("system").tag("system")
                                Text("light").tag("light")
                                Text("dark").tag("dark")
                            }
                            .pickerStyle(.segmented)
                            .labelsHidden()
                        }

                        LabeledContent("language") {
                            Picker("language", selection: $settings.language) {
                                Text("english").tag("en")
                                Text("spanish").tag("es")
                                Text("french").tag("fr")
                                Text("german").tag("de")
                                Text("japanese").tag("ja")
                                Text("chinese").tag("zh")
                            }
                        }
                    }
                    .padding(8)
                }

                // Notifications Section
                GroupBox("notifications") {
                    VStack(alignment: .leading, spacing: 12) {
                        Toggle("enabled", isOn: $settings.notifications.enabled)
                        Toggle("sound", isOn: $settings.notifications.sound)
                            .disabled(!settings.notifications.enabled)
                    }
                    .padding(8)
                }

                // MenuBar Section
                GroupBox("menubar") {
                    VStack(alignment: .leading, spacing: 12) {
                        Toggle("show status icon", isOn: $settings.menubar.showStatus)

                        HStack {
                            Text("refresh interval (seconds)")
                            Slider(value: Binding(
                                get: { Double(settings.menubar.refreshInterval) },
                                set: { settings.menubar.refreshInterval = Int($0) }
                            ), in: 1...60, step: 1)
                            TextField("", value: $settings.menubar.refreshInterval, format: .number)
                                .frame(width: 50)
                                .textFieldStyle(.roundedBorder)
                        }
                    }
                    .padding(8)
                }

                Spacer()
            }
            .padding(8)
        }
    }

    private var rawEditorView: some View {
        VStack(spacing: 8) {
            TextEditor(text: $config)
                .font(.system(size: 12, design: .monospaced))
                .padding(8)
                .background(Color(NSColor.controlBackgroundColor))
                .cornerRadius(4)

            Text("raw json5 editor - comments preserved")
                .font(.caption)
                .foregroundColor(.secondary)
        }
    }

    private func parseSettings() {
        // Strip comments and parse JSON
        var content = config
        content = stripComments(content)

        guard let data = content.data(using: .utf8) else { return }

        do {
            let decoder = JSONDecoder()
            settings = try decoder.decode(ClientSettings.self, from: data)
        } catch {
            // Use defaults if parsing fails
            settings = .default
        }
    }

    private func saveSettings() {
        // Generate JSON5 from settings
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .withoutEscapingSlashes]

        guard let data = try? encoder.encode(settings),
              let json = String(data: data, encoding: .utf8) else {
            return
        }

        // Add comments back for JSON5
        let json5WithComments = addComments(to: json)
        onSave(json5WithComments)
        showValidationSuccess = true

        DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
            showValidationSuccess = false
        }
    }

    private func stripComments(_ content: String) -> String {
        var result = ""
        for line in content.components(separatedBy: "\n") {
            var inString = false
            var escaped = false
            var commentStart: String.Index?

            for (index, char) in line.enumerated() {
                let lineIndex = line.index(line.startIndex, offsetBy: index)

                if escaped {
                    escaped = false
                    continue
                }

                if char == "\\" {
                    escaped = true
                    continue
                }

                if char == "\"" {
                    inString.toggle()
                    continue
                }

                if !inString && char == "/" {
                    let nextIndex = line.index(after: lineIndex)
                    if nextIndex < line.endIndex && line[nextIndex] == "/" {
                        commentStart = lineIndex
                        break
                    }
                }
            }

            if let start = commentStart {
                result += String(line[..<start]) + "\n"
            } else {
                result += line + "\n"
            }
        }
        return result
    }

    private func addComments(to json: String) -> String {
        var result = """
        {
          // Meept Client Configuration
          // This file configures the CLI and menubar app behavior

        """

        // Parse and rebuild with comments
        let lines = json.components(separatedBy: "\n")
        for line in lines {
            if line.contains("\"theme\"") {
                result += "  " + line + "  // ui theme: system, light, dark\n"
            } else if line.contains("\"language\"") {
                result += "  " + line + "  // ui language: en, es, fr, de, ja, zh\n"
            } else if line.contains("\"notifications\"") {
                result += "  " + line + "\n"
            } else if line.contains("\"enabled\"") && json.contains("\"notifications\"") {
                result += "    " + line + "    // enable notifications\n"
            } else if line.contains("\"sound\"") {
                result += "    " + line + "    // play notification sounds\n"
            } else if line.contains("\"menubar\"") {
                result += "  " + line + "\n"
            } else if line.contains("\"show_status\"") {
                result += "    " + line + "    // show daemon status in menubar\n"
            } else if line.contains("\"refresh_interval\"") {
                result += "    " + line + "    // status refresh interval in seconds\n"
            } else {
                result += "  " + line + "\n"
            }
        }

        result += "}"
        return result
    }
}

#Preview {
    ClientConfigView(
        config: .constant("// Loading..."),
        isSaving: false,
        onSave: { _ in }
    )
}
