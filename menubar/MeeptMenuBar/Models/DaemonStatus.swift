//
//  DaemonStatus.swift
//  MeeptMenuBar
//

import Foundation

enum DaemonState: String, Codable {
    case offline = "offline"
    case idle = "idle"
    case working = "working"
    case error = "error"
}

struct DaemonStatus: Codable {
    var running: Bool
    var pid: Int
    var uptime: String
    var state: DaemonState

    init(running: Bool = false, pid: Int = 0, uptime: String = "", state: DaemonState = .offline) {
        self.running = running
        self.pid = pid
        self.uptime = uptime
        self.state = state
    }
}
