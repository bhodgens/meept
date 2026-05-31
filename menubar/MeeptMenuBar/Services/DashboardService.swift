//
//  DashboardService.swift
//  MeeptMenuBar
//

import Foundation

class DashboardService {
    private let baseURL: URL
    private let apiToken: String?

    init() {
        let config = MenubarConfigService()
        self.baseURL = URL(string: config.daemonBaseURL) ?? URL(string: "https://localhost:8081")!
        self.apiToken = config.apiToken
    }

    func getLiveMetrics(completion: @escaping (Result<LiveMetrics, Error>) -> Void) {
        let request = makeRequest(path: "/api/v1/metrics/live", method: "GET")
        perform(request: request, completion: completion) { data in
            try JSONDecoder().decode(LiveMetrics.self, from: data)
        }
    }

    func getHistoricalMetrics(
        from: String, to: String, resolution: String,
        completion: @escaping (Result<[MetricPoint], Error>) -> Void
    ) {
        var components = URLComponents(
            url: baseURL.appendingPathComponent("/api/v1/metrics/historical"),
            resolvingAgainstBaseURL: true
        )
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
        if let token = apiToken, !token.isEmpty {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        perform(request: request, completion: completion) { data in
            let result = try JSONDecoder().decode([String: [MetricPoint]].self, from: data)
            return result["points"] ?? []
        }
    }

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
        }.resume()
    }

    private func extractErrorMessage(from data: Data?) -> String? {
        guard let data = data else { return nil }
        if let json = try? JSONSerialization.jsonObject(with: data) as? [String: String] {
            return json["message"] ?? json["error"]
        }
        return String(data: data, encoding: .utf8)
    }
}
