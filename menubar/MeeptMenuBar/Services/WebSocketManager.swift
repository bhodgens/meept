//
//  WebSocketManager.swift
//  MeeptMenuBar
//

import Foundation
import os.log

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
    private let logger = Logger(subsystem: "com.caimlas.meept.menubar", category: "WebSocketManager")
    var onMessage: ((Data) -> Void)?
    var onDisconnect: ((Error?) -> Void)?
    var onConnect: (() -> Void)?

    init(url: String = "wss://localhost:8081", apiToken: String? = nil) {
        // Fall back to the canonical localhost URL if the configured value
        // is malformed. Avoids a force-unwrap crash on user-controlled
        // menubar.json5 input.
        if let parsed = URL(string: url) {
            self.baseURL = parsed
        } else {
            self.baseURL = URL(string: "wss://localhost:8081")!
        }
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
        guard let session = urlSession else {
            isConnecting = false
            return
        }
        isConnecting = true
        shouldReconnect = true
        // Explicit (re)connect resets the backoff window. Without this, a
        // user-triggered reconnect after an extended outage would still be
        // throttled by the prior reconnectAttempts counter and effectively
        // never fire.
        reconnectAttempts = 0

        var request = URLRequest(url: baseURL.appendingPathComponent("/ws/notifications"))
        if let token = apiToken, !token.isEmpty {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }

        webSocketTask = session.webSocketTask(with: request)
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
        webSocketTask?.send(wsMessage) { [weak self] error in
            if let error = error {
                self?.logger.error("websocket send error: \(error.localizedDescription)")
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
                logger.error("websocket receive error: \(error.localizedDescription)")
                self.handleDisconnect(error: error)
            }
        }
    }

    private func handleDisconnect(error: Error?) {
        isConnecting = false
        onDisconnect?(error)

        guard shouldReconnect, reconnectAttempts < maxReconnectAttempts else {
            logger.info("websocket: max reconnect attempts reached or reconnect disabled")
            return
        }

        // Exponential backoff with jitter to avoid thundering herd on
        // daemon restart when multiple clients reconnect simultaneously.
        let baseDelay = min(
            baseReconnectDelay * pow(2.0, Double(reconnectAttempts)),
            maxReconnectDelay
        )
        let jitter = Double.random(in: 0..<baseDelay * 0.5)
        let delay = baseDelay + jitter
        reconnectAttempts += 1

        logger.info("websocket: reconnecting in \(delay)s (attempt \(self.reconnectAttempts)/\(self.maxReconnectAttempts))")

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
        logger.info("websocket: connected")
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
        logger.info("websocket: disconnected with code \(closeCode.rawValue)")
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
