//
//  SettingsWindow.swift
//  MeeptMenuBar
//

import SwiftUI

@MainActor
struct SettingsWindow: View {
    @State private var selectedTab = 0

    @ObservedObject var configViewModel: ConfigViewModel

    var body: some View {
        TabView(selection: $selectedTab) {
            ClientConfigView(
                config: Binding(
                    get: { configViewModel.clientConfig },
                    set: { configViewModel.clientConfig = $0 }
                ),
                isSaving: configViewModel.isSaving,
                onSave: { content in configViewModel.saveClientConfig(content: content) },
                onAppear: { configViewModel.loadClientConfig() }
            )
            .tabItem {
                Label("client", systemImage: "gearshape")
            }
            .tag(0)

            ModelsConfigView(
                config: Binding(
                    get: { configViewModel.modelsConfig },
                    set: { configViewModel.modelsConfig = $0 }
                ),
                isSaving: configViewModel.isSaving,
                onSave: { content in configViewModel.saveModelsConfig(content: content) }
            )
            .tabItem {
                Label("models", systemImage: "cpu")
            }
            .tag(1)

            AgentsConfigView(configViewModel: configViewModel)
            .tabItem {
                Label("agents", systemImage: "person.crop.circle")
            }
            .tag(2)
        }
        .frame(width: 600, height: 450)
        .padding()
        .alert("save error", isPresented: Binding(
            get: { configViewModel.showSaveError },
            set: { configViewModel.showSaveError = $0 }
        )) {
            Button("ok", role: .cancel) { }
        } message: {
            Text("Failed to save configuration. Please try again.")
        }
        .alert("normalization error", isPresented: Binding(
            get: { configViewModel.showNormalizeError },
            set: { configViewModel.showNormalizeError = $0 }
        )) {
            Button("ok", role: .cancel) { }
        } message: {
            Text("Failed to normalize JSON5. Please check your syntax.")
        }
        .onAppear {
            configViewModel.loadConfigs()
        }
    }
}

#Preview {
    SettingsWindow(configViewModel: ConfigViewModel(configService: ConfigService()))
}
