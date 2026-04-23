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
        request.httpBody = content.data(using: .utf8)
        
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
        request.httpBody = content.data(using: .utf8)
        
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
