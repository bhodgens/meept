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

class MemoryResult {
  /// Returns a new [MemoryResult] instance.
  MemoryResult({
    required this.memory,
    required this.relevanceScore,
    required this.source_,
  });

  Object memory;

  num relevanceScore;

  String source_;

  @override
  bool operator ==(Object other) => identical(this, other) || other is MemoryResult &&
    other.memory == memory &&
    other.relevanceScore == relevanceScore &&
    other.source_ == source_;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (memory.hashCode) +
    (relevanceScore.hashCode) +
    (source_.hashCode);

  @override
  String toString() => 'MemoryResult[memory=$memory, relevanceScore=$relevanceScore, source_=$source_]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'memory'] = this.memory;
      json[r'relevance_score'] = this.relevanceScore;
      json[r'source'] = this.source_;
    return json;
  }

  /// Returns a new [MemoryResult] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static MemoryResult? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'memory'), 'Required key "MemoryResult[memory]" is missing from JSON.');
        assert(json[r'memory'] != null, 'Required key "MemoryResult[memory]" has a null value in JSON.');
        assert(json.containsKey(r'relevance_score'), 'Required key "MemoryResult[relevance_score]" is missing from JSON.');
        assert(json[r'relevance_score'] != null, 'Required key "MemoryResult[relevance_score]" has a null value in JSON.');
        assert(json.containsKey(r'source'), 'Required key "MemoryResult[source]" is missing from JSON.');
        assert(json[r'source'] != null, 'Required key "MemoryResult[source]" has a null value in JSON.');
        return true;
      }());

      return MemoryResult(
        memory: mapValueOfType<Object>(json, r'memory')!,
        relevanceScore: num.parse('${json[r'relevance_score']}'),
        source_: mapValueOfType<String>(json, r'source')!,
      );
    }
    return null;
  }

  static List<MemoryResult> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <MemoryResult>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = MemoryResult.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, MemoryResult> mapFromJson(dynamic json) {
    final map = <String, MemoryResult>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = MemoryResult.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of MemoryResult-objects as value to a dart map
  static Map<String, List<MemoryResult>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<MemoryResult>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = MemoryResult.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'memory',
    'relevance_score',
    'source',
  };
}

