//
//  DaemonController.swift
//  MeeptMenuBar
//

import Foundation

class DaemonController {
    private let launchAgentLabel = "com.caimlas.meept-daemon"
    private let plistPath: String

    init() {
        let homeDir = FileManager.default.homeDirectoryForCurrentUser
        self.plistPath = homeDir
            .appendingPathComponent("Library")
            .appendingPathComponent("LaunchAgents")
            .appendingPathComponent("com.caimlas.meept-daemon.plist")
            .path
    }

    // MARK: - async/await

    func startDaemon() async throws {
        if !ensurePlistExists() {
            throw DaemonError.plistNotFound
        }

        let loadResult = runLaunchctl("load", "-w", plistPath)
        if !loadResult.success {
            throw DaemonError.loadFailed(loadResult.output)
        }

        _ = runLaunchctl("kickstart", "-k", launchAgentLabel)
    }

    func stopDaemon() async throws {
        _ = runLaunchctl("stop", launchAgentLabel)
        let unloadResult = runLaunchctl("unload", "-w", plistPath)
        if !unloadResult.success {
            throw DaemonError.unloadFailed(unloadResult.output)
        }
    }

    func restartDaemon() async throws {
        try await stopDaemon()
        try await Task.sleep(nanoseconds: 500_000_000) // 0.5s
        try await startDaemon()
    }

    // MARK: - Backward-compatible completion handler wrappers

    func startDaemon(completion: @escaping (Result<Void, Error>) -> Void) {
        Task { do { completion(.success(try await startDaemon())) } catch { completion(.failure(error)) } }
    }

    func stopDaemon(completion: @escaping (Result<Void, Error>) -> Void) {
        Task { do { completion(.success(try await stopDaemon())) } catch { completion(.failure(error)) } }
    }

    func restartDaemon(completion: @escaping (Result<Void, Error>) -> Void) {
        Task { do { completion(.success(try await restartDaemon())) } catch { completion(.failure(error)) } }
    }

    // MARK: - Private helpers

    private func ensurePlistExists() -> Bool {
        let fm = FileManager.default
        if fm.fileExists(atPath: plistPath) { return true }

        let plistDir = (plistPath as NSString).deletingLastPathComponent
        try? fm.createDirectory(atPath: plistDir, withIntermediateDirectories: true)

        let plist = generatePlist()
        guard let data = plist.data(using: .utf8) else { return false }
        return fm.createFile(atPath: plistPath, contents: data)
    }

    private func generatePlist() -> String {
        let daemonPath = findDaemonBinary() ?? "/usr/local/bin/meept-daemon"
        let homeDir = FileManager.default.homeDirectoryForCurrentUser.path
        return """
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>\(launchAgentLabel)</string>
    <key>ProgramArguments</key>
    <array>
        <string>\(daemonPath)</string>
        <string>-f</string>
    </array>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>StandardOutPathString</key><string>\(homeDir)/.meept/daemon.log</string>
    <key>StandardErrorPathString</key><string>\(homeDir)/.meept/daemon.err</string>
</dict>
</plist>
"""
    }

    private func findDaemonBinary() -> String? {
        let locations = [
            "./bin/meept-daemon",
            "\(FileManager.default.homeDirectoryForCurrentUser.path)/bin/meept-daemon",
            "/usr/local/bin/meept-daemon",
            "/opt/homebrew/bin/meept-daemon"
        ]
        for path in locations {
            if FileManager.default.fileExists(atPath: path) {
                return (path as NSString).expandingTildeInPath
            }
        }
        return nil
    }

    private func runLaunchctl(_ args: String...) -> (success: Bool, output: String) {
        let process = Process()
        process.launchPath = "/bin/launchctl"
        process.arguments = args

        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe

        do {
            try process.run()
            process.waitUntilExit()
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            let output = String(data: data, encoding: .utf8) ?? ""
            return (process.terminationStatus == 0, output)
        } catch {
            return (false, error.localizedDescription)
        }
    }
}

enum DaemonError: LocalizedError {
    case plistNotFound
    case loadFailed(String)
    case unloadFailed(String)

    var errorDescription: String? {
        switch self {
        case .plistNotFound: return "launchd plist not found"
        case .loadFailed(let output): return "Failed to load: \(output)"
        case .unloadFailed(let output): return "Failed to unload: \(output)"
        }
    }
}
