//
//  DaemonStatus.swift
//  MeeptMenuBar
//

import Foundation

struct DaemonStatus: Codable {
    var running: Bool
    var pid: Int
    var uptime: String

    init(running: Bool = false, pid: Int = 0, uptime: String = "") {
        self.running = running
        self.pid = pid
        self.uptime = uptime
    }
}
