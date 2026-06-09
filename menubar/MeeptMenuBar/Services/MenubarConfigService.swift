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

            // Minimal JSON5 cleanup for bootstrap config parsing.
            // The full normalizer has been removed; user-facing normalization
            // is handled server-side via /api/v1/config/normalize.
            let cleanJSON = MenubarConfigService.stripJSON5Comments(content)
            guard let cleanData = cleanJSON.data(using: .utf8) else { return }

            let decoder = JSONDecoder()
            self.config = try decoder.decode(MenubarConfig.self, from: cleanData)
        } catch {
            // On parse error, keep defaults
            print("Failed to load menubar config: \(error)")
        }
    }

    /// Minimal JSON5 stripping for bootstrap config parsing only.
    /// Removes `//` line comments and trailing commas before `}` / `]`.
    /// Full normalization is done server-side for user-facing edits.
    private static func stripJSON5Comments(_ input: String) -> String {
        // Remove // line comments (not inside strings)
        var result = ""
        var inString = false
        var escape = false
        let chars = Array(input)
        var i = 0

        while i < chars.count {
            let ch = chars[i]
            if escape {
                result.append(ch)
                escape = false
                i += 1
                continue
            }
            if ch == "\\" && inString {
                result.append(ch)
                escape = true
                i += 1
                continue
            }
            if ch == "\"" {
                inString = !inString
                result.append(ch)
                i += 1
                continue
            }
            if !inString && ch == "/" && i + 1 < chars.count && chars[i + 1] == "/" {
                // Skip to end of line
                i += 2
                while i < chars.count && chars[i] != "\n" {
                    i += 1
                }
                continue
            }
            if !inString && ch == "/" && i + 1 < chars.count && chars[i + 1] == "*" {
                // Skip block comment
                i += 2
                while i < chars.count {
                    if chars[i] == "*" && i + 1 < chars.count && chars[i + 1] == "/" {
                        i += 2
                        break
                    }
                    i += 1
                }
                continue
            }
            result.append(ch)
            i += 1
        }

        // Remove trailing commas before } or ]
        var output = ""
        inString = false
        escape = false
        i = 0
        while i < result.count {
            let ch = result[result.index(result.startIndex, offsetBy: i)]
            if escape {
                output.append(ch)
                escape = false
                i += 1
                continue
            }
            if ch == "\\" && inString {
                output.append(ch)
                escape = true
                i += 1
                continue
            }
            if ch == "\"" {
                inString = !inString
                output.append(ch)
                i += 1
                continue
            }
            if !inString && ch == "," {
                // Look ahead for } or ] skipping whitespace
                var j = i + 1
                while j < result.count {
                    let ahead = result[result.index(result.startIndex, offsetBy: j)]
                    if ahead.isWhitespace {
                        j += 1
                        continue
                    }
                    if ahead == "}" || ahead == "]" {
                        // Skip the comma
                        i += 1
                        while i < j {
                            output.append(result[result.index(result.startIndex, offsetBy: i)])
                            i += 1
                        }
                        output.append(ahead)
                        i = j + 1
                        break
                    }
                    // Not a trailing comma, keep it
                    output.append(ch)
                    i += 1
                    break
                }
                if j >= result.count {
                    output.append(ch)
                    i += 1
                }
                continue
            }
            output.append(ch)
            i += 1
        }

        return output
    }
}
