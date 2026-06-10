//
//  WebSocketManager.swift
//  MeeptMenuBar
//

import Foundation

/// WebSocket manager with auto-reconnection and exponential backoff.
class WebSocketManager: NSObject {
    private var webSocketTask: URLSessionWebSocketTask?
    private var urlSession: URLSession?
    private let baseURL: URL
    private let apiToken: String?

    // Retry configuration
    private let maxReconnectAttempts = 10
    private let baseReconnectDelay: TimeInterval = 1.0
    private let maxReconnectDelay: TimeInterval = 60.0

    private var reconnectAttempts = 0
    private var isConnecting = false
    private var shouldReconnect = true

    // Callbacks
    var onMessage: ((Data) -> Void)?
    var onDisconnect: ((Error?) -> Void)?
    var onConnect: (() -> Void)?

    init(url: String = "wss://localhost:8081", apiToken: String? = nil) {
        self.baseURL = URL(string: url)!
        self.apiToken = apiToken
        super.init()

        // Use same TLS config as APIClient for consistency
        let config = URLSessionConfiguration.ephemeral
        config.tlsMinimumSupportedProtocolVersion = .TLSv12
        self.urlSession = URLSession(
            configuration: config,
            delegate: self,
            delegateQueue: .main
        )
    }

    // MARK: - Public API

    func connect() {
        guard !isConnecting else { return }
        isConnecting = true
        shouldReconnect = true

        var request = URLRequest(url: baseURL.appendingPathComponent("/ws/notifications"))
        if let token = apiToken, !token.isEmpty {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }

        webSocketTask = urlSession?.webSocketTask(with: request)
        webSocketTask?.resume()
        receiveMessage()
    }

    func disconnect() {
        shouldReconnect = false
        webSocketTask?.cancel(with: .normalClosure, reason: nil)
        webSocketTask = nil
        isConnecting = false
    }

    func send(_ message: String) {
        let wsMessage = URLSessionWebSocketTask.Message.string(message)
        webSocketTask?.send(wsMessage) { error in
            if let error = error {
                print("WebSocket send error: \(error)")
            }
        }
    }

    // MARK: - Private

    private func receiveMessage() {
        webSocketTask?.receive { [weak self] result in
            guard let self = self else { return }

            switch result {
            case .success(let message):
                switch message {
                case .string(let text):
                    if let data = text.data(using: .utf8) {
                        self.onMessage?(data)
                    }
                case .data(let data):
                    self.onMessage?(data)
                @unknown default:
                    break
                }
                // Continue listening
                self.receiveMessage()

            case .failure(let error):
                print("WebSocket receive error: \(error)")
                self.handleDisconnect(error: error)
            }
        }
    }

    private func handleDisconnect(error: Error?) {
        isConnecting = false
        onDisconnect?(error)

        guard shouldReconnect, reconnectAttempts < maxReconnectAttempts else {
            print("WebSocket: max reconnect attempts reached or reconnect disabled")
            return
        }

        // Exponential backoff
        let delay = min(
            baseReconnectDelay * pow(2.0, Double(reconnectAttempts)),
            maxReconnectDelay
        )
        reconnectAttempts += 1

        print("WebSocket: reconnecting in \(delay)s (attempt \(reconnectAttempts)/\(maxReconnectAttempts))")

        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
            self?.connect()
        }
    }
}

// MARK: - URLSessionWebSocketDelegate

extension WebSocketManager: URLSessionWebSocketDelegate {
    func urlSession(
        _ session: URLSession,
        webSocketTask: URLSessionWebSocketTask,
        didOpenWithProtocol protocol: String?
    ) {
        print("WebSocket: connected")
        isConnecting = true
        reconnectAttempts = 0
        onConnect?()
    }

    func urlSession(
        _ session: URLSession,
        webSocketTask: URLSessionWebSocketTask,
        didCloseWith closeCode: URLSessionWebSocketTask.CloseCode,
        reason: Data?
    ) {
        print("WebSocket: disconnected with code \(closeCode)")
        handleDisconnect(error: nil)
    }

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
        // Accept self-signed certs for localhost
        if host == "localhost" || host == "127.0.0.1" || host == "::1" {
            let credential = URLCredential(trust: serverTrust)
            completionHandler(.useCredential, credential)
        } else {
            completionHandler(.performDefaultHandling, nil)
        }
    }
}