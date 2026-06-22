//
//  SettingsWindow.swift
//  MeeptMenuBar
//

import SwiftUI

@MainActor
struct SettingsWindow: View {
    @State private var selectedTab = 0

    @ObservedObject var configViewModel: ConfigViewModel
    @StateObject private var mcpViewModel: MCPViewModel
    @StateObject private var memoryReviewViewModel: MemoryReviewViewModel

    init(
        configViewModel: ConfigViewModel,
        mcpViewModel: MCPViewModel? = nil,
        memoryReviewViewModel: MemoryReviewViewModel? = nil
    ) {
        self.configViewModel = configViewModel
        // Allow callers to inject an APIClient-backed MCPViewModel (the main
        // AppDelegate path). Fall back to a fresh APIClient for previews /
        // older call sites that don't supply one.
        if let mcpViewModel {
            self._mcpViewModel = StateObject(wrappedValue: mcpViewModel)
        } else {
            let config = MenubarConfigService()
            let api = APIClient(
                baseURL: config.daemonBaseURL,
                apiToken: config.apiToken
            )
            self._mcpViewModel = StateObject(wrappedValue: MCPViewModel(api: api))
        }
        // Same injected-or-default pattern for the memory review tab.
        if let memoryReviewViewModel {
            self._memoryReviewViewModel = StateObject(wrappedValue: memoryReviewViewModel)
        } else {
            let config = MenubarConfigService()
            let api = APIClient(
                baseURL: config.daemonBaseURL,
                apiToken: config.apiToken
            )
            self._memoryReviewViewModel = StateObject(wrappedValue: MemoryReviewViewModel(api: api))
        }
    }

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

            MCPServersView(viewModel: mcpViewModel)
            .tabItem {
                Label("tools", systemImage: "wrench.and.screwdriver")
            }
            .tag(3)

            MemoryReviewView(viewModel: memoryReviewViewModel)
            .tabItem {
                Label("memory", systemImage: "brain.head.profile")
            }
            .tag(4)
        }
        .frame(width: 600, height: 450)
        .padding()
        .alert("save error", isPresented: Binding(
            get: { configViewModel.showSaveError },
            set: { configViewModel.showSaveError = $0 }
        )) {
            Button("ok", role: .cancel) { }
        } message: {
            Text("failed to save configuration. please try again.")
        }
        .alert("normalization error", isPresented: Binding(
            get: { configViewModel.showNormalizeError },
            set: { configViewModel.showNormalizeError = $0 }
        )) {
            Button("ok", role: .cancel) { }
        } message: {
            Text("failed to normalize JSON5. please check your syntax.")
        }
        .onAppear {
            configViewModel.loadConfigs()
        }
    }
}

#Preview {
    SettingsWindow(configViewModel: ConfigViewModel(configService: ConfigService()))
}
