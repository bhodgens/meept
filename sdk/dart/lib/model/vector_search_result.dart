//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

// Standalone model file
import 'dart:convert' show json;
import 'package:collection/collection.dart';

class VectorSearchResult {
  /// Returns a new [VectorSearchResult] instance.
  VectorSearchResult({
    required this.memoryId,
    required this.content,
    this.metadataCommaOmitempty,
    required this.relevanceScore,
    required this.vectorSimilarity,
  });

  String memoryId;

  String content;

  String? metadataCommaOmitempty;

  num relevanceScore;

  num vectorSimilarity;

  @override
  bool operator ==(Object other) => identical(this, other) || other is VectorSearchResult &&
    other.memoryId == memoryId &&
    other.content == content &&
    other.metadataCommaOmitempty == metadataCommaOmitempty &&
    other.relevanceScore == relevanceScore &&
    other.vectorSimilarity == vectorSimilarity;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (memoryId.hashCode) +
    (content.hashCode) +
    (metadataCommaOmitempty == null ? 0 : metadataCommaOmitempty!.hashCode) +
    (relevanceScore.hashCode) +
    (vectorSimilarity.hashCode);

  @override
  String toString() => 'VectorSearchResult[memoryId=$memoryId, content=$content, metadataCommaOmitempty=$metadataCommaOmitempty, relevanceScore=$relevanceScore, vectorSimilarity=$vectorSimilarity]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'memory_id'] = this.memoryId;
      json[r'content'] = this.content;
    if (this.metadataCommaOmitempty != null) {
      json[r'metadata,omitempty'] = this.metadataCommaOmitempty;
    } else {
      json[r'metadata,omitempty'] = null;
    }
      json[r'relevance_score'] = this.relevanceScore;
      json[r'vector_similarity'] = this.vectorSimilarity;
    return json;
  }

  /// Returns a new [VectorSearchResult] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static VectorSearchResult? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'memory_id'), 'Required key "VectorSearchResult[memory_id]" is missing from JSON.');
        assert(json[r'memory_id'] != null, 'Required key "VectorSearchResult[memory_id]" has a null value in JSON.');
        assert(json.containsKey(r'content'), 'Required key "VectorSearchResult[content]" is missing from JSON.');
        assert(json[r'content'] != null, 'Required key "VectorSearchResult[content]" has a null value in JSON.');
        assert(json.containsKey(r'relevance_score'), 'Required key "VectorSearchResult[relevance_score]" is missing from JSON.');
        assert(json[r'relevance_score'] != null, 'Required key "VectorSearchResult[relevance_score]" has a null value in JSON.');
        assert(json.containsKey(r'vector_similarity'), 'Required key "VectorSearchResult[vector_similarity]" is missing from JSON.');
        assert(json[r'vector_similarity'] != null, 'Required key "VectorSearchResult[vector_similarity]" has a null value in JSON.');
        return true;
      }());

      return VectorSearchResult(
        memoryId: mapValueOfType<String>(json, r'memory_id')!,
        content: mapValueOfType<String>(json, r'content')!,
        metadataCommaOmitempty: mapValueOfType<String>(json, r'metadata,omitempty'),
        relevanceScore: num.parse('${json[r'relevance_score']}'),
        vectorSimilarity: num.parse('${json[r'vector_similarity']}'),
      );
    }
    return null;
  }

  static List<VectorSearchResult> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <VectorSearchResult>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = VectorSearchResult.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, VectorSearchResult> mapFromJson(dynamic json) {
    final map = <String, VectorSearchResult>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = VectorSearchResult.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of VectorSearchResult-objects as value to a dart map
  static Map<String, List<VectorSearchResult>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<VectorSearchResult>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = VectorSearchResult.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'memory_id',
    'content',
    'relevance_score',
    'vector_similarity',
  };
}

