//
//  SettingsWindow.swift
//  MeeptMenuBar
//

import SwiftUI

struct SettingsWindow: View {
    @State private var selectedTab = 0
    @State private var clientConfig = "// Loading..."
    @State private var modelsConfig = "// Loading..."
    @State private var isSaving = false
    @State private var showSaveError = false
    @State private var showNormalizeError = false

    private let configService = ConfigService()

    var body: some View {
        TabView(selection: $selectedTab) {
            ClientConfigView(
                config: $clientConfig,
                isSaving: isSaving,
                onSave: { content in
                    isSaving = true
                    configService.normalizeJSON5(content: content) { result in
                        switch result {
                        case .success:
                            // Validated via normalize; save the user's original
                            // text to preserve comments and formatting.
                            configService.saveClientConfig(content: content) { saveResult in
                                isSaving = false
                                switch saveResult {
                                case .success:
                                    break
                                case .failure:
                                    showSaveError = true
                                }
                            }
                        case .failure:
                            isSaving = false
                            showNormalizeError = true
                        }
                    }
                }
            )
            .tabItem {
                Label("client", systemImage: "gearshape")
            }
            .tag(0)

            ModelsConfigView(
                config: $modelsConfig,
                isSaving: isSaving,
                onSave: { content in
                    isSaving = true
                    configService.normalizeJSON5(content: content) { result in
                        switch result {
                        case .success:
                            // Validated via normalize; save the user's original
                            // text to preserve comments and formatting.
                            configService.saveModelsConfig(content: content) { saveResult in
                                isSaving = false
                                switch saveResult {
                                case .success:
                                    break
                                case .failure:
                                    showSaveError = true
                                }
                            }
                        case .failure:
                            isSaving = false
                            showNormalizeError = true
                        }
                    }
                }
            )
            .tabItem {
                Label("models", systemImage: "cpu")
            }
            .tag(1)

            AgentsConfigView()
            .tabItem {
                Label("agents", systemImage: "person.crop.circle")
            }
            .tag(2)
        }
        .frame(width: 600, height: 450)
        .padding()
        .alert("save error", isPresented: $showSaveError) {
            Button("ok", role: .cancel) { }
        } message: {
            Text("Failed to save configuration. Please try again.")
        }
        .alert("normalization error", isPresented: $showNormalizeError) {
            Button("ok", role: .cancel) { }
        } message: {
            Text("Failed to normalize JSON5. Please check your syntax.")
        }
        .onAppear {
            loadConfigs()
        }
    }

    private func loadConfigs() {
        configService.getClientConfig { result in
            switch result {
            case .success(let content):
                clientConfig = content
            case .failure:
                clientConfig = "// Error loading config"
            }
        }

        configService.getModelsConfig { result in
            switch result {
            case .success(let content):
                modelsConfig = content
            case .failure:
                modelsConfig = "// Error loading config"
            }
        }
    }
}

#Preview {
    SettingsWindow()
}
