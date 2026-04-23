//
//  DashboardService.swift
//  MeeptMenuBar
//

import Foundation

class DashboardService {
    private let baseURL: URL
    
    init(baseURL: String = "http://localhost:8081") {
        self.baseURL = URL(string: baseURL)!
    }
    
    func getLiveMetrics(completion: @escaping (Result<LiveMetrics, Error>) -> Void) {
        let url = baseURL.appendingPathComponent("/api/v1/metrics/live")
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
                let metrics = try JSONDecoder().decode(LiveMetrics.self, from: data)
                completion(.success(metrics))
            } catch {
                completion(.failure(error))
            }
        }.resume()
    }
    
    func getHistoricalMetrics(from: String, to: String, resolution: String, completion: @escaping (Result<[MetricPoint], Error>) -> Void) {
        var components = URLComponents(url: baseURL.appendingPathComponent("/api/v1/metrics/historical"), resolvingAgainstBaseURL: true)
        components?.queryItems = [
            URLQueryItem(name: "from", value: from),
            URLQueryItem(name: "to", value: to),
            URLQueryItem(name: "resolution", value: resolution)
        ]
        
        guard let url = components?.url else {
            completion(.failure(APIError.invalidURL))
            return
        }
        
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
                let result = try JSONDecoder().decode([String: [MetricPoint]].self, from: data)
                completion(.success(result["points"] ?? []))
            } catch {
                completion(.failure(error))
            }
        }.resume()
    }
}
