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

    // MARK: - async/await

    func getLiveMetrics() async throws -> LiveMetrics {
        let request = makeRequest(path: "/api/v1/metrics/live", method: "GET")
        let data = try await performData(request: request)
        return try JSONDecoder().decode(LiveMetrics.self, from: data)
    }

    func getHistoricalMetrics(from: String, to: String, resolution: String) async throws -> [MetricPoint] {
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
            throw APIError.invalidURL
        }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        if let token = apiToken, !token.isEmpty {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        let data = try await performData(request: request)
        let result = try JSONDecoder().decode([String: [MetricPoint]].self, from: data)
        return result["points"] ?? []
    }

    // MARK: - Backward-compatible completion handler wrappers

    func getLiveMetrics(completion: @escaping (Result<LiveMetrics, Error>) -> Void) {
        Task { do { completion(.success(try await getLiveMetrics())) } catch { completion(.failure(error)) } }
    }

    func getHistoricalMetrics(
        from: String, to: String, resolution: String,
        completion: @escaping (Result<[MetricPoint], Error>) -> Void
    ) {
        Task { do { completion(.success(try await getHistoricalMetrics(from: from, to: to, resolution: resolution))) } catch { completion(.failure(error)) } }
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
        let (data, response) = try await URLSession.shared.data(for: request)
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

    private func extractErrorMessage(from data: Data?) -> String? {
        guard let data = data else { return nil }
        if let json = try? JSONSerialization.jsonObject(with: data) as? [String: String] {
            return json["message"] ?? json["error"]
        }
        return String(data: data, encoding: .utf8)
    }
}
