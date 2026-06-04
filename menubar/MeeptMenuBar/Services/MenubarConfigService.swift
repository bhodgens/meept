//
//  MenubarConfigService.swift
//  MeeptMenuBar
//

import Foundation

/// Default development API key shared with the daemon and other clients.
/// In production, replace via `meept token generate --save`.
let DefaultDevAPIKey = "meept_dev_default_key_CHANGE_ME"

struct MenubarConfig: Codable {
    var daemon: DaemonConfig
    var ui: UIConfig
    var notifications: NotificationsConfig

    struct DaemonConfig: Codable {
        var httpURL: String
        var apiToken: String?

        enum CodingKeys: String, CodingKey {
            case httpURL = "http_url"
            case apiToken = "api_token"
        }
    }

    struct UIConfig: Codable {
        var showInMenuBar: Bool
        var startAtLogin: Bool
        var iconStyle: String

        enum CodingKeys: String, CodingKey {
            case showInMenuBar = "show_in_menu_bar"
            case startAtLogin = "start_at_login"
            case iconStyle = "icon_style"
        }
    }

    struct NotificationsConfig: Codable {
        var enabled: Bool
        var level: String
    }
}

extension MenubarConfig {
    static let `default` = MenubarConfig(
        daemon: DaemonConfig(httpURL: "https://localhost:8081", apiToken: nil),
        ui: UIConfig(showInMenuBar: true, startAtLogin: false, iconStyle: "icon"),
        notifications: NotificationsConfig(enabled: true, level: "errors_only")
    )
}

class MenubarConfigService {
    private let fileURL: URL
    private var config: MenubarConfig = .default

    init() {
        let homeDir = FileManager.default.homeDirectoryForCurrentUser
        self.fileURL = homeDir.appendingPathComponent(".meept/menubar.json5")
        loadConfig()
    }

    var daemonBaseURL: String {
        return config.daemon.httpURL
    }

    var apiToken: String? {
        return config.daemon.apiToken ?? DefaultDevAPIKey
    }

    var showInMenuBar: Bool {
        return config.ui.showInMenuBar
    }

    var startAtLogin: Bool {
        return config.ui.startAtLogin
    }

    var notificationsEnabled: Bool {
        return config.notifications.enabled
    }

    var notificationsLevel: String {
        return config.notifications.level
    }

    private func loadConfig() {
        guard FileManager.default.fileExists(atPath: fileURL.path) else {
            // No config file, use defaults
            return
        }

        do {
            let data = try Data(contentsOf: fileURL)
            guard let content = String(data: data, encoding: .utf8) else { return }

            // Convert JSON5 → strict JSON so JSONDecoder can parse it
            let cleanJSON = try JSON5Normalizer.normalize(content)
            guard let cleanData = cleanJSON.data(using: .utf8) else { return }

            let decoder = JSONDecoder()
            self.config = try decoder.decode(MenubarConfig.self, from: cleanData)
        } catch {
            // On parse error, keep defaults
            print("Failed to load menubar config: \(error)")
        }
    }
}
