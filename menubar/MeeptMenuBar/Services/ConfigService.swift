//
//  ConfigService.swift
//  MeeptMenuBar
//

import Foundation

class ConfigService {
    private let baseURL: URL
    private let apiToken: String?

    init() {
        let config = MenubarConfigService()
        self.baseURL = URL(string: config.daemonBaseURL) ?? URL(string: "https://localhost:8081")!
        self.apiToken = config.apiToken
    }

    // MARK: - Client Config

    func getClientConfig(completion: @escaping (Result<String, Error>) -> Void) {
        let request = makeRequest(path: "/api/v1/config/client", method: "GET")
        performString(request: request, completion: completion)
    }

    func saveClientConfig(content: String, completion: @escaping (Result<Void, Error>) -> Void) {
        var request = makeRequest(path: "/api/v1/config/client", method: "POST")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let body: [String: String] = ["content": content]
        request.httpBody = try? JSONSerialization.data(withJSONObject: body)
        performVoid(request: request, completion: completion)
    }

    // MARK: - Models Config

    func getModelsConfig(completion: @escaping (Result<String, Error>) -> Void) {
        let request = makeRequest(path: "/api/v1/config/models", method: "GET")
        performString(request: request, completion: completion)
    }

    func saveModelsConfig(content: String, completion: @escaping (Result<Void, Error>) -> Void) {
        var request = makeRequest(path: "/api/v1/config/models", method: "POST")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let body: [String: String] = ["content": content]
        request.httpBody = try? JSONSerialization.data(withJSONObject: body)
        performVoid(request: request, completion: completion)
    }

    // MARK: - Agents

    func getAgentsList(completion: @escaping (Result<[AgentInfo], Error>) -> Void) {
        let request = makeRequest(path: "/api/v1/config/agents", method: "GET")
        perform(request: request, completion: completion) { data in
            let decoder = JSONDecoder()
            let result = try decoder.decode(AgentsListResponse.self, from: data)
            return result.agents
        }
    }

    func getAgent(id: String, completion: @escaping (Result<Agent, Error>) -> Void) {
        let request = makeRequest(path: "/api/v1/config/agents/\(id)", method: "GET")
        perform(request: request, completion: completion) { data in
            let decoder = JSONDecoder()
            return try decoder.decode(Agent.self, from: data)
        }
    }

    func saveAgent(id: String, agent: Agent, completion: @escaping (Result<Void, Error>) -> Void) {
        var request = makeRequest(path: "/api/v1/config/agents/\(id)", method: "POST")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try? JSONEncoder().encode(agent)
        performVoid(request: request, completion: completion)
    }

    // MARK: - Menubar Config

    func getMenubarConfig(completion: @escaping (Result<String, Error>) -> Void) {
        let request = makeRequest(path: "/api/v1/config/menubar", method: "GET")
        performString(request: request, completion: completion)
    }

    func saveMenubarConfig(content: String, completion: @escaping (Result<Void, Error>) -> Void) {
        var request = makeRequest(path: "/api/v1/config/menubar", method: "POST")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let body: [String: String] = ["content": content]
        request.httpBody = try? JSONSerialization.data(withJSONObject: body)
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
        URLSession.shared.dataTask(with: request) { data, response, error in
            if let error = error {
                DispatchQueue.main.async {
                    completion(.failure(APIError.networkError(error.localizedDescription)))
                }
                return
            }
            guard let httpResponse = response as? HTTPURLResponse else {
                DispatchQueue.main.async {
                    completion(.failure(APIError.invalidResponse))
                }
                return
            }
            guard (200..<300).contains(httpResponse.statusCode) else {
                let body = self.extractErrorMessage(from: data)
                DispatchQueue.main.async {
                    completion(.failure(APIError.httpError(httpResponse.statusCode, body)))
                }
                return
            }
            guard let data = data else {
                DispatchQueue.main.async {
                    completion(.failure(APIError.invalidResponse))
                }
                return
            }
            do {
                let result = try parse(data)
                DispatchQueue.main.async {
                    completion(.success(result))
                }
            } catch {
                DispatchQueue.main.async {
                    completion(.failure(APIError.decodingError(error.localizedDescription)))
                }
            }
        }.resume()
    }

    private func performVoid(
        request: URLRequest,
        completion: @escaping (Result<Void, Error>) -> Void
    ) {
        URLSession.shared.dataTask(with: request) { data, response, error in
            DispatchQueue.main.async {
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
        }.resume()
    }

    private func performString(
        request: URLRequest,
        completion: @escaping (Result<String, Error>) -> Void
    ) {
        perform(request: request, completion: completion) { data in
            String(data: data, encoding: .utf8) ?? ""
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
