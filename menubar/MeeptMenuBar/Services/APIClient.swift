//
//  APIClient.swift
//  MeeptMenuBar
//

import Foundation

class APIClient {
    private let baseURL: URL
    private let session: URLSession

    init(baseURL: String = "http://localhost:8081") {
        self.baseURL = URL(string: baseURL)!
        self.session = URLSession(configuration: .ephemeral)
    }

    func getDaemonStatus(completion: @escaping (Result<DaemonStatus, Error>) -> Void) {
        let url = baseURL.appendingPathComponent("/api/v1/daemon/status")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"

        let task = session.dataTask(with: request) { data, response, error in
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
                let status = try decoder.decode(DaemonStatusResponse.self, from: data)
                completion(.success(DaemonStatus(
                    running: status.running,
                    pid: status.pid,
                    uptime: status.uptime
                )))
            } catch {
                completion(.failure(error))
            }
        }
        task.resume()
    }

    func restartDaemon(completion: @escaping (Result<Void, Error>) -> Void) {
        let url = baseURL.appendingPathComponent("/api/v1/daemon/restart")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"

        let task = session.dataTask(with: request) { data, response, error in
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
        }
        task.resume()
    }
}

struct DaemonStatusResponse: Codable {
    let running: Bool
    let pid: Int
    let uptime: String
}

enum APIError: LocalizedError {
    case invalidURL
    case invalidResponse
    case httpError(Int)

    var errorDescription: String? {
        switch self {
        case .invalidURL: return "Invalid URL"
        case .invalidResponse: return "Invalid response from server"
        case .httpError(let code): return "HTTP error: \(code)"
        }
    }
}
