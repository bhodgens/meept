//
//  ConfigService.swift
//  MeeptMenuBar
//

import Foundation

class ConfigService {
    private let baseURL: URL
    private let apiToken: String?
    private let session: URLSession

    init() {
        let config = MenubarConfigService()
        self.baseURL = URL(string: config.daemonBaseURL) ?? URL(string: "https://localhost:8081")!
        self.apiToken = config.apiToken

        // Accept self-signed certs for localhost (same as APIClient)
        let sessionConfig = URLSessionConfiguration.ephemeral
        sessionConfig.tlsMinimumSupportedProtocolVersion = .TLSv12
        self.session = URLSession(
            configuration: sessionConfig,
            delegate: LocalhostTrustDelegate.shared,
            delegateQueue: nil
        )
    }

    // MARK: - Client Config (async/await)

    func getClientConfig() async throws -> String {
        let request = makeRequest(path: "/api/v1/config/client", method: "GET")
        let data = try await performData(request: request)
        return String(data: data, encoding: .utf8) ?? ""
    }

    func saveClientConfig(content: String) async throws {
        var request = makeRequest(path: "/api/v1/config/client", method: "POST")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let body: [String: String] = ["content": content]
        request.httpBody = try? JSONSerialization.data(withJSONObject: body)
        try await performVoid(request: request)
    }

    // MARK: - Models Config (async/await)

    func getModelsConfig() async throws -> String {
        let request = makeRequest(path: "/api/v1/config/models", method: "GET")
        let data = try await performData(request: request)
        return String(data: data, encoding: .utf8) ?? ""
    }

    func saveModelsConfig(content: String) async throws {
        var request = makeRequest(path: "/api/v1/config/models", method: "POST")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let body: [String: String] = ["content": content]
        request.httpBody = try? JSONSerialization.data(withJSONObject: body)
        try await performVoid(request: request)
    }

    // MARK: - Agents (async/await)

    func getAgentsList() async throws -> [AgentInfo] {
        let request = makeRequest(path: "/api/v1/config/agents", method: "GET")
        let data = try await performData(request: request)
        let decoder = JSONDecoder()
        let result = try decoder.decode(AgentsListResponse.self, from: data)
        return result.agents
    }

    func getAgent(id: String) async throws -> Agent {
        let request = makeRequest(path: "/api/v1/config/agents/\(id)", method: "GET")
        let data = try await performData(request: request)
        let decoder = JSONDecoder()
        return try decoder.decode(Agent.self, from: data)
    }

    func saveAgent(id: String, agent: Agent) async throws {
        var request = makeRequest(path: "/api/v1/config/agents/\(id)", method: "POST")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try? JSONEncoder().encode(agent)
        try await performVoid(request: request)
    }

    // MARK: - Menubar Config (async/await)

    func getMenubarConfig() async throws -> String {
        let request = makeRequest(path: "/api/v1/config/menubar", method: "GET")
        let data = try await performData(request: request)
        return String(data: data, encoding: .utf8) ?? ""
    }

    func saveMenubarConfig(content: String) async throws {
        var request = makeRequest(path: "/api/v1/config/menubar", method: "POST")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let body: [String: String] = ["content": content]
        request.httpBody = try? JSONSerialization.data(withJSONObject: body)
        try await performVoid(request: request)
    }

    // MARK: - JSON5 Normalization (async/await)

    func normalizeJSON5(content: String) async throws -> String {
        var request = makeRequest(path: "/api/v1/config/normalize", method: "POST")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let body: [String: String] = ["content": content]
        request.httpBody = try? JSONSerialization.data(withJSONObject: body)
        let data = try await performData(request: request)
        let json = try JSONSerialization.jsonObject(with: data) as? [String: String]
        guard let normalized = json?["normalized"] else {
            throw APIError.decodingError("missing 'normalized' field in response")
        }
        return normalized
    }

    // MARK: - Backward-compatible completion handler wrappers

    func getClientConfig(completion: @escaping (Result<String, Error>) -> Void) {
        Task { do { completion(.success(try await getClientConfig())) } catch { completion(.failure(error)) } }
    }

    func saveClientConfig(content: String, completion: @escaping (Result<Void, Error>) -> Void) {
        Task { do { completion(.success(try await saveClientConfig(content: content))) } catch { completion(.failure(error)) } }
    }

    func getModelsConfig(completion: @escaping (Result<String, Error>) -> Void) {
        Task { do { completion(.success(try await getModelsConfig())) } catch { completion(.failure(error)) } }
    }

    func saveModelsConfig(content: String, completion: @escaping (Result<Void, Error>) -> Void) {
        Task { do { completion(.success(try await saveModelsConfig(content: content))) } catch { completion(.failure(error)) } }
    }

    func getAgentsList(completion: @escaping (Result<[AgentInfo], Error>) -> Void) {
        Task { do { completion(.success(try await getAgentsList())) } catch { completion(.failure(error)) } }
    }

    func getAgent(id: String, completion: @escaping (Result<Agent, Error>) -> Void) {
        Task { do { completion(.success(try await getAgent(id: id))) } catch { completion(.failure(error)) } }
    }

    func saveAgent(id: String, agent: Agent, completion: @escaping (Result<Void, Error>) -> Void) {
        Task { do { completion(.success(try await saveAgent(id: id, agent: agent))) } catch { completion(.failure(error)) } }
    }

    func getMenubarConfig(completion: @escaping (Result<String, Error>) -> Void) {
        Task { do { completion(.success(try await getMenubarConfig())) } catch { completion(.failure(error)) } }
    }

    func saveMenubarConfig(content: String, completion: @escaping (Result<Void, Error>) -> Void) {
        Task { do { completion(.success(try await saveMenubarConfig(content: content))) } catch { completion(.failure(error)) } }
    }

    func normalizeJSON5(content: String, completion: @escaping (Result<String, Error>) -> Void) {
        Task { do { completion(.success(try await normalizeJSON5(content: content))) } catch { completion(.failure(error)) } }
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
