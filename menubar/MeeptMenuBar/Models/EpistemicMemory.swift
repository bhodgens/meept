//
//  EpistemicMemory.swift
//  MeeptMenuBar
//
//  Codable models for the epistemic memory review-queue UI. Mirrors the
//  daemon's `MemoryResult` shape returned by `GET /api/v1/memory/review-queue`.
//  The daemon returns `MemoryResult` objects (memory + relevance + source) for
//  each of the three queues, so we decode the nested `memory` block and
//  flatten the fields we need into Identifiable row models.
//

import AnyCodable
import Foundation

// MARK: - Wire types (mirror the daemon JSON)

/// Wrapper for the `GET /api/v1/memory/review-queue` response.
struct ReviewQueue: Codable, Equatable {
    let autoClaims: [MemoryResult]
    let pendingDecisions: [MemoryResult]
    let pendingPredictions: [MemoryResult]

    enum CodingKeys: String, CodingKey {
        case autoClaims = "auto_claims"
        case pendingDecisions = "pending_decisions"
        case pendingPredictions = "pending_predictions"
    }
}

/// Mirrors `internal/memory/types.go MemoryResult`. The daemon serializes
/// each queue entry as a MemoryResult regardless of underlying memory type.
struct MemoryResult: Codable, Identifiable, Equatable {
    let memory: MemoryEntry
    let relevanceScore: Double
    let source: String?

    var id: String { memory.id }

    enum CodingKeys: String, CodingKey {
        case memory
        case relevanceScore = "relevance_score"
        case source
    }
}

/// Mirrors `internal/memory/types.go Memory`. Only the fields surfaced in
/// the review-queue UI are decoded; metadata is kept as a loose dictionary
/// because claims, decisions, and predictions each store different keys.
struct MemoryEntry: Codable, Equatable {
    let id: String
    let content: String
    let type: String
    let category: String?
    let metadata: [String: AnyCodable]?
    let createdAt: String?

    enum CodingKeys: String, CodingKey {
        case id
        case content
        case type
        case category
        case metadata
        case createdAt = "created_at"
    }
}

// MARK: - Convenience accessors

extension MemoryResult {
    /// Returns the `status` metadata field, or "" if absent.
    var status: String {
        memory.metadata?["status"]?.value as? String ?? ""
    }

    /// Returns the `confidence` metadata field as a Double, or nil.
    var confidence: Double? {
        if let n = memory.metadata?["confidence"]?.value as? Double {
            return n
        }
        if let i = memory.metadata?["confidence"]?.value as? Int {
            return Double(i)
        }
        return nil
    }

    /// Returns the `expected_outcome` metadata field, or nil.
    var expectedOutcome: String? {
        memory.metadata?["expected_outcome"]?.value as? String
    }

    /// Returns the `review_at` metadata field, or nil.
    var reviewDue: String? {
        memory.metadata?["review_at"]?.value as? String
    }

    /// Returns the `horizon` metadata field, or nil.
    var horizon: String? {
        if let s = memory.metadata?["horizon"]?.value as? String {
            return s
        }
        return nil
    }
}
