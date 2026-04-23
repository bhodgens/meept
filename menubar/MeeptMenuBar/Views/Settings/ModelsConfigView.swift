//
//  ModelsConfigView.swift
//  MeeptMenuBar
//

import SwiftUI

struct ModelsConfigView: View {
    @Binding var config: String
    @State private var modelsConfig: ModelsConfig = .default
    @State private var presets: [ModelPreset] = []
    @State private var selectedProviderId: String?
    @State private var selectedModelId: String?
    @State private var showRawEditor = false
    @State private var showValidationSuccess = false
    @State private var activeTab = 0 // 0 = models, 1 = presets
    @State private var selectedPresetId: String?
    let isSaving: Bool
    let onSave: (String) -> Void

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("models configuration")
                    .font(.headline)
                Spacer()
                Picker("tab", selection: $activeTab) {
                    Text("models").tag(0)
                    Text("presets").tag(1)
                }
                .pickerStyle(.segmented)
                .frame(width: 150)
                Button("raw json") {
                    showRawEditor.toggle()
                }
                .buttonStyle(.borderless)
                if isSaving {
                    ProgressView()
                        .scaleEffect(0.8)
                }
                Button("save") {
                    saveConfig()
                }
                .keyboardShortcut("s", modifiers: .command)
                .buttonStyle(.borderedProminent)
            }
            .padding(8)

            Divider()

            if showRawEditor {
                rawEditorView
            } else if activeTab == 1 {
                presetsView
            } else {
                modelsView
            }
        }
        .padding(8)
        .onAppear {
            parseConfig()
            loadPresets()
        }
    }

    private var modelsView: some View {
        HSplitView {
            // Left panel - Provider/Model list
            VStack(spacing: 0) {
                HStack {
                    Text("providers")
                        .font(.headline)
                    Spacer()
                }
                .padding(8)

                Divider()

                // Global settings
                List {
                    Section("global settings") {
                        HStack {
                            Text("default model")
                            Spacer()
                            TextField("", text: $modelsConfig.model)
                                .frame(width: 150)
                                .textFieldStyle(.roundedBorder)
                        }

                        HStack {
                            Text("small model")
                            Spacer()
                            TextField("", text: $modelsConfig.smallModel)
                                .frame(width: 150)
                                .textFieldStyle(.roundedBorder)
                        }

                        HStack {
                            Text("default timeout")
                            Spacer()
                            TextField("", value: $modelsConfig.defaultTimeout, format: .number)
                                .frame(width: 80)
                                .textFieldStyle(.roundedBorder)
                            Text("s")
                                .foregroundColor(.secondary)
                        }
                    }

                    Section("providers") {
                        ForEach(Array(modelsConfig.providers.keys).sorted(), id: \.self) { providerId in
                            Button(providerId) {
                                selectedProviderId = providerId
                                selectedModelId = nil
                            }
                            .buttonStyle(.plain)
                            .contentShape(Rectangle())
                        }
                    }
                }
                .listStyle(.sidebar)
            }
            .frame(minWidth: 200, idealWidth: 220)

            // Right panel - Model editor
            VStack(spacing: 0) {
                if let providerId = selectedProviderId,
                   let provider = modelsConfig.providers[providerId] {

                    // Provider header
                    HStack {
                        Text("\(providerId)")
                            .font(.headline)
                        Spacer()
                        Toggle("disabled", isOn: Binding(
                            get: { modelsConfig.disabledProviders.contains(providerId) },
                            set: { isDisabled in
                                if isDisabled {
                                    if !modelsConfig.disabledProviders.contains(providerId) {
                                        modelsConfig.disabledProviders.append(providerId)
                                    }
                                } else {
                                    modelsConfig.disabledProviders.removeAll { $0 == providerId }
                                }
                            }
                        ))
                        .toggleStyle(.switch)
                    }
                    .padding(8)

                    Divider()

                    // Provider options
                    ScrollView {
                        VStack(alignment: .leading, spacing: 16) {
                            // API Type - use Binding to update the model
                            LabeledContent("api type") {
                                TextField("", text: Binding(
                                    get: { modelsConfig.providers[providerId]?.api ?? "" },
                                    set: { modelsConfig.providers[providerId]?.api = $0 }
                                ))
                                .textFieldStyle(.roundedBorder)
                            }

                            Divider()

                            // Options
                            GroupBox("options") {
                                VStack(alignment: .leading, spacing: 12) {
                                    LabeledContent("base url") {
                                        TextField("https://api.example.com", text: Binding(
                                            get: { modelsConfig.providers[providerId]?.options.baseURL ?? "" },
                                            set: { modelsConfig.providers[providerId]?.options.baseURL = $0 }
                                        ))
                                        .textFieldStyle(.roundedBorder)
                                    }

                                    LabeledContent("api key") {
                                        SecureField("${API_KEY}", text: Binding(
                                            get: { modelsConfig.providers[providerId]?.options.apiKey ?? "" },
                                            set: { modelsConfig.providers[providerId]?.options.apiKey = $0 }
                                        ))
                                        .textFieldStyle(.roundedBorder)
                                    }

                                    HStack {
                                        Text("timeout")
                                        TextField("", value: Binding(
                                            get: { modelsConfig.providers[providerId]?.options.timeout ?? 30 },
                                            set: { modelsConfig.providers[providerId]?.options.timeout = $0 }
                                        ), format: .number)
                                            .frame(width: 80)
                                            .textFieldStyle(.roundedBorder)
                                        Text("seconds")
                                            .foregroundColor(.secondary)
                                    }
                                }
                                .padding(8)
                            }

                            Divider()

                            // Models list
                            GroupBox("models") {
                                VStack(alignment: .leading, spacing: 12) {
                                    ForEach(Array(provider.models.keys).sorted(), id: \.self) { modelId in
                                        ModelEditorRow(
                                            modelId: modelId,
                                            model: provider.models[modelId]!,
                                            presets: presets,
                                            provider: providerId,
                                            onSave: { updatedModel in
                                                modelsConfig.providers[providerId]?.models[modelId] = updatedModel
                                            }
                                        )
                                        .padding(.vertical, 4)
                                    }
                                }
                                .padding(8)
                            }
                        }
                        .padding(8)
                    }
                } else {
                    VStack(spacing: 16) {
                        Image(systemName: "cpu")
                            .font(.system(size: 48))
                            .foregroundColor(.secondary)
                        Text("select a provider to configure models")
                            .foregroundColor(.secondary)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .frame(minWidth: 350)
        }
    }

    private var presetsView: some View {
        HSplitView {
            // Left panel - Preset list
            VStack(spacing: 0) {
                HStack {
                    Text("presets")
                        .font(.headline)
                    Spacer()
                    Button("add") {
                        addPreset()
                    }
                    .buttonStyle(.bordered)
                }
                .padding(8)

                Divider()

                List(presets, selection: Binding(
                    get: { presets.firstIndex { $0.id == selectedPresetId } },
                    set: { index in
                        if let idx = index {
                            selectedPresetId = presets[idx].id
                        }
                    }
                )) { preset in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(preset.label)
                            .font(.system(size: 13))
                        Text(preset.description)
                            .font(.system(size: 10))
                            .foregroundColor(.secondary)
                    }
                    .tag(preset.id)
                }
                .listStyle(.sidebar)
            }
            .frame(minWidth: 180, idealWidth: 200)

            // Right panel - Preset editor
            VStack(spacing: 0) {
                if let preset = presets.first(where: { $0.id == selectedPresetId }) {
                    PresetEditor(
                        preset: preset,
                        presets: presets,
                        onSave: { updated in
                            if let idx = presets.firstIndex(where: { $0.id == updated.id }) {
                                presets[idx] = updated
                            }
                        }
                    )
                } else {
                    VStack(spacing: 16) {
                        Image(systemName: "slider.horizontal.3")
                            .font(.system(size: 48))
                            .foregroundColor(.secondary)
                        Text("select a preset to edit")
                            .foregroundColor(.secondary)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
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

    private func parseConfig() {
        var content = config
        content = stripComments(content)

        guard let data = content.data(using: .utf8) else { return }

        do {
            let decoder = JSONDecoder()
            modelsConfig = try decoder.decode(ModelsConfig.self, from: data)
        } catch {
            modelsConfig = .default
        }
    }

    private func loadPresets() {
        // Load built-in presets
        presets = [
            ModelPreset(id: "development", label: "Development", description: "Balanced for coding",
                       params: ModelPresetParams(temperature: 0.3, topP: 0.9, frequencyPenalty: 0, presencePenalty: 0, maxTokens: nil)),
            ModelPreset(id: "debugging", label: "Debugging", description: "Methodical troubleshooting",
                       params: ModelPresetParams(temperature: 0.2, topP: 0.85, frequencyPenalty: 0, presencePenalty: 0, maxTokens: nil)),
            ModelPreset(id: "planning", label: "Planning", description: "Structured thinking",
                       params: ModelPresetParams(temperature: 0.4, topP: 0.9, frequencyPenalty: 0, presencePenalty: 0, maxTokens: nil)),
            ModelPreset(id: "creative", label: "Creative", description: "High creativity mode",
                       params: ModelPresetParams(temperature: 0.9, topP: 0.95, frequencyPenalty: 0.5, presencePenalty: 0.5, maxTokens: nil)),
            ModelPreset(id: "research", label: "Research", description: "Analytical and thorough",
                       params: ModelPresetParams(temperature: 0.5, topP: 0.9, frequencyPenalty: 0, presencePenalty: 0, maxTokens: nil)),
            ModelPreset(id: "fast", label: "Fast", description: "Quick responses",
                       params: ModelPresetParams(temperature: 0.3, topP: 0.8, frequencyPenalty: 0, presencePenalty: 0, maxTokens: 1000)),
            ModelPreset(id: "detailed", label: "Detailed", description: "Comprehensive answers",
                       params: ModelPresetParams(temperature: 0.5, topP: 0.9, frequencyPenalty: 0, presencePenalty: 0, maxTokens: 8000))
        ]
    }

    private func addPreset() {
        let newId = "preset_\(Int(Date().timeIntervalSince1970))"
        presets.append(ModelPreset(
            id: newId,
            label: "New Preset",
            description: "Custom preset",
            params: ModelPresetParams(temperature: 0.7, topP: 0.9, frequencyPenalty: 0, presencePenalty: 0, maxTokens: nil)
        ))
        selectedPresetId = newId
    }

    private func saveConfig() {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .withoutEscapingSlashes]

        guard let data = try? encoder.encode(modelsConfig),
              let json = String(data: data, encoding: .utf8) else {
            return
        }

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
        var result = "{\n"
        result += "  // Meept Models Configuration\n"
        result += "  // Define LLM providers and models with their capabilities\n\n"

        let lines = json.components(separatedBy: "\n")
        for line in lines {
            var processedLine = line

            if line.contains("\"model\"") && !line.contains("small_model") {
                processedLine = "  " + line + "  // Default model for general use"
            } else if line.contains("\"small_model\"") {
                processedLine = "  " + line + "  // Fast model for classification"
            } else if line.contains("\"default_timeout\"") {
                processedLine = "  " + line + "  // Default timeout in seconds"
            }

            result += processedLine + "\n"
        }

        result += "}"
        return result
    }
}

// MARK: - Model Editor Row

struct ModelEditorRow: View {
    let modelId: String
    let model: ModelsConfig.ModelDefinition
    let presets: [ModelPreset]
    let provider: String
    let onSave: (ModelsConfig.ModelDefinition) -> Void

    @State private var isExpanded = false
    @State private var name: String
    @State private var preset: String?
    @State private var temperature: Double
    @State private var topP: Double
    @State private var contextLimit: Int
    @State private var maxOutput: Int

    init(modelId: String, model: ModelsConfig.ModelDefinition, presets: [ModelPreset], provider: String, onSave: @escaping (ModelsConfig.ModelDefinition) -> Void) {
        self.modelId = modelId
        self.model = model
        self.presets = presets
        self.provider = provider
        self.onSave = onSave
        _name = State(initialValue: model.name)
        _preset = State(initialValue: model.preset)
        _temperature = State(initialValue: model.temperature)
        _topP = State(initialValue: model.topP ?? 0.9)
        _contextLimit = State(initialValue: model.contextLimit)
        _maxOutput = State(initialValue: model.maxOutput)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Button(action: { isExpanded.toggle() }) {
                    Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                        .font(.system(size: 10))
                        .foregroundColor(.secondary)
                }
                .buttonStyle(.plain)

                Text(modelId)
                    .font(.system(size: 13, weight: .medium))

                Spacer()

                if let preset = preset {
                    Text(preset)
                        .font(.system(size: 10))
                        .padding(.horizontal, 4)
                        .padding(.vertical, 2)
                        .background(Color.blue.opacity(0.2))
                        .cornerRadius(3)
                }

                Text("\(contextLimit / 1000)K context")
                    .font(.system(size: 10))
                    .foregroundColor(.secondary)
            }

            if isExpanded {
                Divider()

                VStack(alignment: .leading, spacing: 12) {
                    LabeledContent("display name") {
                        TextField("", text: $name)
                            .textFieldStyle(.roundedBorder)
                            .onSubmit { save() }
                    }

                    LabeledContent("preset") {
                        Picker("preset", selection: $preset) {
                            Text("none").tag(nil as String?)
                            ForEach(presets, id: \.id) { p in
                                Text(p.label).tag(p.id as String?)
                            }
                        }
                        .onChange(of: preset) { _ in save() }
                    }

                    HStack {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("temperature")
                                .font(.caption)
                            Slider(value: $temperature, in: 0...2, step: 0.1)
                                .onChange(of: preset) { _ in save() }
                            Text(String(format: "%.1f", temperature))
                                .font(.caption)
                                .foregroundColor(.secondary)
                        }

                        VStack(alignment: .leading, spacing: 4) {
                            Text("top_p")
                                .font(.caption)
                            Slider(value: $topP, in: 0...1, step: 0.05)
                                .onChange(of: preset) { _ in save() }
                            Text(String(format: "%.2f", topP))
                                .font(.caption)
                                .foregroundColor(.secondary)
                        }
                    }

                    HStack {
                        LabeledContent("context limit") {
                            TextField("", value: $contextLimit, format: .number)
                                .frame(width: 80)
                                .textFieldStyle(.roundedBorder)
                                .onSubmit { save() }
                        }

                        LabeledContent("max output") {
                            TextField("", value: $maxOutput, format: .number)
                                .frame(width: 80)
                                .textFieldStyle(.roundedBorder)
                                .onSubmit { save() }
                        }
                    }

                    Text("capabilities: \(model.capabilities.joined(separator: ", "))")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
                .padding(8)
                .background(Color(NSColor.controlBackgroundColor))
                .cornerRadius(4)
            }
        }
        .padding(4)
    }

    private func save() {
        var updated = model
        updated.name = name
        updated.preset = preset
        updated.temperature = temperature
        updated.topP = topP
        updated.contextLimit = contextLimit
        updated.maxOutput = maxOutput
        onSave(updated)
    }
}

// MARK: - Preset Editor

struct PresetEditor: View {
    var preset: ModelPreset
    let presets: [ModelPreset]
    let onSave: (ModelPreset) -> Void

    @State private var label: String = ""
    @State private var description: String = ""
    @State private var temperature: Double = 0.7
    @State private var topP: Double = 0.9
    @State private var frequencyPenalty: Double = 0
    @State private var presencePenalty: Double = 0
    @State private var maxTokens: Int?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                GroupBox("preset info") {
                    VStack(alignment: .leading, spacing: 12) {
                        LabeledContent("label") {
                            TextField("", text: $label)
                                .textFieldStyle(.roundedBorder)
                        }

                        LabeledContent("description") {
                            TextField("", text: $description)
                                .textFieldStyle(.roundedBorder)
                        }
                    }
                    .padding(8)
                }

                GroupBox("parameters") {
                    VStack(alignment: .leading, spacing: 12) {
                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                Text("temperature")
                                    .font(.caption)
                                Spacer()
                                Text(String(format: "%.2f", temperature))
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                            }
                            Slider(value: $temperature, in: 0...2, step: 0.05)
                        }

                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                Text("top_p")
                                    .font(.caption)
                                Spacer()
                                Text(String(format: "%.2f", topP))
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                            }
                            Slider(value: $topP, in: 0...1, step: 0.05)
                        }

                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                Text("frequency_penalty")
                                    .font(.caption)
                                Spacer()
                                Text(String(format: "%.2f", frequencyPenalty))
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                            }
                            Slider(value: $frequencyPenalty, in: 0...2, step: 0.05)
                        }

                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                Text("presence_penalty")
                                    .font(.caption)
                                Spacer()
                                Text(String(format: "%.2f", presencePenalty))
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                            }
                            Slider(value: $presencePenalty, in: 0...2, step: 0.05)
                        }

                        HStack {
                            Text("max_tokens (optional)")
                                .font(.caption)
                            Spacer()
                            TextField("", value: $maxTokens, format: .number)
                                .frame(width: 80)
                                .textFieldStyle(.roundedBorder)
                        }
                    }
                    .padding(8)
                }

                HStack {
                    Spacer()
                    Button("save preset") {
                        savePreset()
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(label.isEmpty)
                }
            }
            .padding(8)
        }
        .onAppear {
            label = preset.label
            description = preset.description
            temperature = preset.params.temperature ?? 0.7
            topP = preset.params.topP ?? 0.9
            frequencyPenalty = preset.params.frequencyPenalty ?? 0
            presencePenalty = preset.params.presencePenalty ?? 0
            maxTokens = preset.params.maxTokens
        }
    }

    private func savePreset() {
        let updated = ModelPreset(
            id: preset.id,
            label: label,
            description: description,
            params: ModelPresetParams(
                temperature: temperature,
                topP: topP,
                frequencyPenalty: frequencyPenalty,
                presencePenalty: presencePenalty,
                maxTokens: maxTokens
            )
        )
        onSave(updated)
    }
}

#Preview {
    ModelsConfigView(
        config: .constant("{\n  \"models\": []\n}"),
        isSaving: false,
        onSave: { _ in }
    )
}
