//
//  ClientSettings.swift
//  MeeptMenuBar
//

import Foundation

struct ClientSettings: Codable {
    var theme: String
    var language: String
    var notifications: NotificationsSettings
    var menubar: MenubarSettings

    struct NotificationsSettings: Codable {
        var enabled: Bool
        var sound: Bool
    }

    struct MenubarSettings: Codable {
        var showStatus: Bool
        var refreshInterval: Int
    }
}

extension ClientSettings {
    static let `default` = ClientSettings(
        theme: "system",
        language: "en",
        notifications: NotificationsSettings(enabled: true, sound: true),
        menubar: MenubarSettings(showStatus: true, refreshInterval: 5)
    )

    init(from data: Data) throws {
        let decoder = JSONDecoder()
        // Handle JSON5-like comments by stripping them
        var content = String(data: data, encoding: .utf8) ?? ""
        content = Self.stripComments(content)
        guard let cleanData = content.data(using: .utf8) else {
            throw DecodingError.dataCorrupted(
                DecodingError.Context(codingPath: [], debugDescription: "Invalid string encoding")
            )
        }
        self = try decoder.decode(ClientSettings.self, from: cleanData)
    }

    private static func stripComments(_ content: String) -> String {
        var result = ""
        for line in content.components(separatedBy: "\n") {
            // Remove // comments but preserve the rest of the line
            if let commentIndex = line.firstIndex(of: "/") {
                let nextIndex = line.index(after: commentIndex)
                if nextIndex < line.endIndex && line[nextIndex] == "/" {
                    result += String(line[..<commentIndex]) + "\n"
                    continue
                }
            }
            result += line + "\n"
        }
        return result
    }
}
