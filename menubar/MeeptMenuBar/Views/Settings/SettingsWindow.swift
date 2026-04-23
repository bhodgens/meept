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
    
    private let configService = ConfigService()
    
    var body: some View {
        TabView(selection: $selectedTab) {
            ClientConfigView(
                config: $clientConfig,
                isSaving: isSaving,
                onSave: { content in
                    isSaving = true
                    configService.saveClientConfig(content: content) { result in
                        isSaving = false
                        switch result {
                        case .success:
                            break
                        case .failure:
                            showSaveError = true
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
                    configService.saveModelsConfig(content: content) { result in
                        isSaving = false
                        switch result {
                        case .success:
                            break
                        case .failure:
                            showSaveError = true
                        }
                    }
                }
            )
            .tabItem {
                Label("models", systemImage: "cpu")
            }
            .tag(1)
        }
        .frame(width: 600, height: 450)
        .padding()
        .alert("Save Error", isPresented: $showSaveError) {
            Button("OK", role: .cancel) { }
        } message: {
            Text("Failed to save configuration. Please try again.")
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
