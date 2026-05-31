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
        self.baseURL = URL(string: baseURL)!
        self.apiToken = apiToken

        // Accept self-signed certs for localhost — matches the Go server's
        // auto-generated cert. TODO: pin the exact cert fingerprint.
        let config = URLSessionConfiguration.ephemeral
        config.tlsMinimumSupportedProtocolVersion = .TLSv12
        self.session = URLSession(
            configuration: config,
            delegate: LocalhostTrustDelegate(),
            delegateQueue: nil
        )
    }

    // MARK: - Daemon Status

    func getDaemonStatus(completion: @escaping (Result<DaemonStatus, Error>) -> Void) {
        var request = makeRequest(path: "/api/v1/daemon/status", method: "GET")
        perform(request: request, completion: completion) { data in
            let decoder = JSONDecoder()
            let status = try decoder.decode(DaemonStatusResponse.self, from: data)
            return DaemonStatus(
                running: status.running,
                pid: status.pid,
                uptime: status.uptime,
                state: DaemonState(rawValue: status.state) ?? .offline
            )
        }
    }

    func restartDaemon(completion: @escaping (Result<Void, Error>) -> Void) {
        var request = makeRequest(path: "/api/v1/daemon/restart", method: "POST")
        performVoid(request: request, completion: completion)
    }

    // MARK: - Private helpers

    private func makeRequest(path: String, method: String) -> URLRequest {
        let url = baseURL.appendingPathComponent(path)
        var request = URLRequest(url: url)
        request.httpMethod = method
        if let token = apiToken, !token.isEmpty {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        return request
    }

    private func perform<T>(
        request: URLRequest,
        completion: @escaping (Result<T, Error>) -> Void,
        parse: @escaping (Data) throws -> T
    ) {
        let task = session.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(APIError.networkError(error.localizedDescription)))
                return
            }
            guard let httpResponse = response as? HTTPURLResponse else {
                completion(.failure(APIError.invalidResponse))
                return
            }
            guard (200..<300).contains(httpResponse.statusCode) else {
                let body = self.extractErrorMessage(from: data)
                completion(.failure(APIError.httpError(httpResponse.statusCode, body)))
                return
            }
            guard let data = data else {
                completion(.failure(APIError.invalidResponse))
                return
            }
            do {
                let result = try parse(data)
                completion(.success(result))
            } catch {
                completion(.failure(APIError.decodingError(error.localizedDescription)))
            }
        }
        task.resume()
    }

    private func performVoid(
        request: URLRequest,
        completion: @escaping (Result<Void, Error>) -> Void
    ) {
        let task = session.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(APIError.networkError(error.localizedDescription)))
                return
            }
            guard let httpResponse = response as? HTTPURLResponse else {
                completion(.failure(APIError.invalidResponse))
                return
            }
            guard (200..<300).contains(httpResponse.statusCode) else {
                let body = self.extractErrorMessage(from: data)
                completion(.failure(APIError.httpError(httpResponse.statusCode, body)))
                return
            }
            completion(.success(()))
        }
        task.resume()
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

class LocalhostTrustDelegate: NSObject, URLSessionDelegate {
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
        }
    }
}
