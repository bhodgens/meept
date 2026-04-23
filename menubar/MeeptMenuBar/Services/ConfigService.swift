//
//  ConfigService.swift
//  MeeptMenuBar
//

import Foundation

class ConfigService {
    private let baseURL: URL
    
    init(baseURL: String = "http://localhost:8081") {
        self.baseURL = URL(string: baseURL)!
    }
    
    func getClientConfig(completion: @escaping (Result<String, Error>) -> Void) {
        let url = baseURL.appendingPathComponent("/api/v1/config/client")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        
        URLSession.shared.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }
            
            guard let httpResponse = response as? HTTPURLResponse,
                  (200..<300).contains(httpResponse.statusCode),
                  let data = data else {
                completion(.failure(APIError.invalidResponse))
                return
            }
            
            completion(.success(String(data: data, encoding: .utf8) ?? ""))
        }.resume()
    }
    
    func saveClientConfig(content: String, completion: @escaping (Result<Void, Error>) -> Void) {
        let url = baseURL.appendingPathComponent("/api/v1/config/client")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        let body: [String: String] = ["content": content]
        request.httpBody = try? JSONSerialization.data(withJSONObject: body)

        URLSession.shared.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }

            guard let httpResponse = response as? HTTPURLResponse,
                  (200..<300).contains(httpResponse.statusCode) else {
                completion(.failure(APIError.invalidResponse))
                return
            }

            completion(.success(()))
        }.resume()
    }
    
    func getModelsConfig(completion: @escaping (Result<String, Error>) -> Void) {
        let url = baseURL.appendingPathComponent("/api/v1/config/models")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        
        URLSession.shared.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }
            
            guard let httpResponse = response as? HTTPURLResponse,
                  (200..<300).contains(httpResponse.statusCode),
                  let data = data else {
                completion(.failure(APIError.invalidResponse))
                return
            }
            
            completion(.success(String(data: data, encoding: .utf8) ?? ""))
        }.resume()
    }
    
    func saveModelsConfig(content: String, completion: @escaping (Result<Void, Error>) -> Void) {
        let url = baseURL.appendingPathComponent("/api/v1/config/models")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        let body: [String: String] = ["content": content]
        request.httpBody = try? JSONSerialization.data(withJSONObject: body)

        URLSession.shared.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }

            guard let httpResponse = response as? HTTPURLResponse,
                  (200..<300).contains(httpResponse.statusCode) else {
                completion(.failure(APIError.invalidResponse))
                return
            }

            completion(.success(()))
        }.resume()
    }

    func getAgentsList(completion: @escaping (Result<[AgentInfo], Error>) -> Void) {
        let url = baseURL.appendingPathComponent("/api/v1/config/agents")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"

        URLSession.shared.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }

            guard let httpResponse = response as? HTTPURLResponse,
                  (200..<300).contains(httpResponse.statusCode),
                  let data = data else {
                completion(.failure(APIError.invalidResponse))
                return
            }

            do {
                let decoder = JSONDecoder()
                let result = try decoder.decode(AgentsListResponse.self, from: data)
                completion(.success(result.agents))
            } catch {
                completion(.failure(error))
            }
        }.resume()
    }

    func getAgent(id: String, completion: @escaping (Result<Agent, Error>) -> Void) {
        let url = baseURL.appendingPathComponent("/api/v1/config/agents/\(id)")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"

        URLSession.shared.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }

            guard let httpResponse = response as? HTTPURLResponse,
                  (200..<300).contains(httpResponse.statusCode),
                  let data = data else {
                completion(.failure(APIError.invalidResponse))
                return
            }

            do {
                let decoder = JSONDecoder()
                let agent = try decoder.decode(Agent.self, from: data)
                completion(.success(agent))
            } catch {
                completion(.failure(error))
            }
        }.resume()
    }

    func saveAgent(id: String, agent: Agent, completion: @escaping (Result<Void, Error>) -> Void) {
        let url = baseURL.appendingPathComponent("/api/v1/config/agents/\(id)")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        let encoder = JSONEncoder()
        request.httpBody = try? encoder.encode(agent)

        URLSession.shared.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }

            guard let httpResponse = response as? HTTPURLResponse,
                  (200..<300).contains(httpResponse.statusCode) else {
                completion(.failure(APIError.invalidResponse))
                return
            }

            completion(.success(()))
        }.resume()
    }
}
