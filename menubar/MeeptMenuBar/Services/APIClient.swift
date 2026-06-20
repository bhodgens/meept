//
//  APIClient.swift
//  MeeptMenuBar
//

import Foundation

class APIClient {
    private let baseURL: URL
    private let apiToken: String?
    private let session: URLSession

    init(baseURL: String = "https://localhost:8081", apiToken: String? = nil) {
        // Fall back to the canonical localhost URL if the configured value
        // is malformed — matches ConfigService/DashboardService behavior and
        // avoids a force-unwrap crash on user-controlled menubar.json5 input.
        if let url = URL(string: baseURL) {
            self.baseURL = url
        } else {
            self.baseURL = URL(string: "https://localhost:8081")!
        }
        self.apiToken = apiToken

        // Accept self-signed certs for localhost — matches the Go server's
        // auto-generated cert. TODO: pin the exact cert fingerprint.
        let config = URLSessionConfiguration.ephemeral
        config.tlsMinimumSupportedProtocolVersion = .TLSv12
        self.session = URLSession(
            configuration: config,
            delegate: LocalhostTrustDelegate.shared,
            delegateQueue: nil
        )
    }

    // MARK: - Health Check (no auth required)

    func checkHealth() async throws -> Bool {
        let request = try makeRequest(path: "/health", method: "GET", requiresAuth: false)
        let (_, response) = try await session.data(for: request)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw APIError.invalidResponse
        }
        return (200..<300).contains(httpResponse.statusCode)
    }

    // MARK: - Daemon Status (async/await)

    func getDaemonStatus() async throws -> DaemonStatus {
        let request = try makeRequest(path: "/api/v1/daemon/status", method: "GET")
        let data = try await performData(request: request)
        let decoder = JSONDecoder()
        let status = try decoder.decode(DaemonStatusResponse.self, from: data)
        return DaemonStatus(
            running: status.running,
            pid: status.pid,
            uptime: status.uptime,
            state: DaemonState(rawValue: status.state) ?? .offline
        )
    }

    func restartDaemon() async throws {
        let request = try makeRequest(path: "/api/v1/daemon/restart", method: "POST")
        try await performVoid(request: request)
    }

    // MARK: - MCP Servers (async/await)

    /// Fetches the full list of configured MCP servers with runtime stats.
    func getMCPServers() async throws -> [MCPServer] {
        let request = try makeRequest(path: "/api/v1/mcp/servers", method: "GET")
        let data = try await performData(request: request)
        let decoder = JSONDecoder()
        let resp = try decoder.decode(MCPServersResponse.self, from: data)
        return resp.servers.map { MCPServer(from: $0) }
    }

    /// Toggles a single MCP server's `enabled` flag. Persists on the daemon
    /// side (atomic write to `mcp_servers.json5` + manager reload) and returns
    /// the updated entry.
    func setMCPEnabled(name: String, enabled: Bool) async throws -> MCPServer {
        var request = try makeRequest(path: "/api/v1/mcp/servers/\(name)/enabled", method: "PUT")
        let body = try JSONEncoder().encode(["enabled": enabled])
        request.httpBody = body
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let data = try await performData(request: request)
        let decoder = JSONDecoder()
        let status = try decoder.decode(MCPServerStatus.self, from: data)
        return MCPServer(from: status)
    }

    // MARK: - Backward-compatible completion handler wrappers

    func getDaemonStatus(completion: @escaping (Result<DaemonStatus, Error>) -> Void) {
        Task {
            do {
                let status = try await getDaemonStatus()
                completion(.success(status))
            } catch {
                completion(.failure(error))
            }
        }
    }

    func restartDaemon(completion: @escaping (Result<Void, Error>) -> Void) {
        Task {
            do {
                try await restartDaemon()
                completion(.success(()))
            } catch {
                completion(.failure(error))
            }
        }
    }

    func getMCPServers(completion: @escaping (Result<[MCPServer], Error>) -> Void) {
        Task {
            do {
                let servers = try await getMCPServers()
                completion(.success(servers))
            } catch {
                completion(.failure(error))
            }
        }
    }

    func setMCPEnabled(
        name: String, enabled: Bool,
        completion: @escaping (Result<MCPServer, Error>) -> Void
    ) {
        Task {
            do {
                let server = try await setMCPEnabled(name: name, enabled: enabled)
                completion(.success(server))
            } catch {
                completion(.failure(error))
            }
        }
    }

    // MARK: - Private helpers

    private func makeRequest(path: String, method: String, requiresAuth: Bool = true) throws -> URLRequest {
        let url = baseURL.appendingPathComponent(path)
        var request = URLRequest(url: url)
        request.httpMethod = method
        if requiresAuth {
            guard let token = apiToken, !token.isEmpty else {
                throw APIError.noAPITokenConfigured
            }
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        return request
    }

    private func performData(request: URLRequest) async throws -> Data {
        let (data, response) = try await session.data(for: request)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw APIError.invalidResponse
        }
        guard (200..<300).contains(httpResponse.statusCode) else {
            let body = extractErrorMessage(from: data)
            throw APIError.httpError(httpResponse.statusCode, body)
        }
        guard !data.isEmpty else {
            throw APIError.invalidResponse
        }
        return data
    }

    private func perform<T>(
        request: URLRequest,
        parse: (Data) throws -> T
    ) async throws -> T {
        let data = try await performData(request: request)
        do {
            return try parse(data)
        } catch let error as APIError {
            throw error
        } catch {
            throw APIError.decodingError(error.localizedDescription)
        }
    }

    private func performVoid(request: URLRequest) async throws {
        let (data, response) = try await session.data(for: request)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw APIError.invalidResponse
        }
        guard (200..<300).contains(httpResponse.statusCode) else {
            let body = extractErrorMessage(from: data)
            throw APIError.httpError(httpResponse.statusCode, body)
        }
    }

    private func extractErrorMessage(from data: Data?) -> String? {
        guard let data = data else { return nil }
        if let json = try? JSONSerialization.jsonObject(with: data) as? [String: String] {
            return json["message"] ?? json["error"]
        }
        return String(data: data, encoding: .utf8)
    }
}

// MARK: - URLSessionDelegate for self-signed localhost certs

class LocalhostTrustDelegate: NSObject, URLSessionDelegate, @unchecked Sendable {
    static let shared = LocalhostTrustDelegate()

    func urlSession(
        _ session: URLSession,
        didReceive challenge: URLAuthenticationChallenge,
        completionHandler: @escaping (URLSession.AuthChallengeDisposition, URLCredential?) -> Void
    ) {
        guard let serverTrust = challenge.protectionSpace.serverTrust,
              let host = challenge.protectionSpace.host as String? else {
            completionHandler(.cancelAuthenticationChallenge, nil)
            return
        }
        // Only accept self-signed certs for localhost
        if host == "localhost" || host == "127.0.0.1" || host == "::1" {
            let credential = URLCredential(trust: serverTrust)
            completionHandler(.useCredential, credential)
        } else {
            completionHandler(.performDefaultHandling, nil)
        }
    }
}

// MARK: - Data models

struct DaemonStatusResponse: Codable {
    let running: Bool
    let pid: Int
    let uptime: String
    let state: String
}

enum APIError: LocalizedError {
    case invalidURL
    case invalidResponse
    case httpError(Int, String?)
    case networkError(String)
    case decodingError(String)
    case noAPITokenConfigured

    var errorDescription: String? {
        switch self {
        case .invalidURL:
            return "invalid URL"
        case .invalidResponse:
            return "invalid response from server"
        case .httpError(let code, let message):
            switch code {
            case 401:
                return "missing API token — add api_token to ~/.meept/menubar.json5"
            case 418:
                return "invalid API token (HTTP 418)"
            case 426:
                return "use HTTPS for this endpoint (HTTP 426)"
            default:
                if let msg = message {
                    return "server error \(code): \(msg)"
                }
                return "server error: \(code)"
            }
        case .networkError(let msg):
            return "network error: \(msg)"
        case .decodingError(let msg):
            return "failed to decode response: \(msg)"
        case .noAPITokenConfigured:
            return "no API token configured — run 'meept token generate --save' or add api_token to ~/.meept/menubar.json5"
        }
    }
}
