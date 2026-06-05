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

}
