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

class CacheInspectResult {
  /// Returns a new [CacheInspectResult] instance.
  CacheInspectResult({
    required this.promptHash,
    required this.modelId,
    required this.createdAt,
    required this.expiresAt,
    required this.hitCount,
    this.fileHashesCommaOmitempty,
    required this.source_,
  });

  String promptHash;

  String modelId;

  String createdAt;

  String expiresAt;

  int hitCount;

  String? fileHashesCommaOmitempty;

  String source_;

  @override
  bool operator ==(Object other) => identical(this, other) || other is CacheInspectResult &&
    other.promptHash == promptHash &&
    other.modelId == modelId &&
    other.createdAt == createdAt &&
    other.expiresAt == expiresAt &&
    other.hitCount == hitCount &&
    other.fileHashesCommaOmitempty == fileHashesCommaOmitempty &&
    other.source_ == source_;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (promptHash.hashCode) +
    (modelId.hashCode) +
    (createdAt.hashCode) +
    (expiresAt.hashCode) +
    (hitCount.hashCode) +
    (fileHashesCommaOmitempty == null ? 0 : fileHashesCommaOmitempty!.hashCode) +
    (source_.hashCode);

  @override
  String toString() => 'CacheInspectResult[promptHash=$promptHash, modelId=$modelId, createdAt=$createdAt, expiresAt=$expiresAt, hitCount=$hitCount, fileHashesCommaOmitempty=$fileHashesCommaOmitempty, source_=$source_]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'prompt_hash'] = this.promptHash;
      json[r'model_id'] = this.modelId;
      json[r'created_at'] = this.createdAt;
      json[r'expires_at'] = this.expiresAt;
      json[r'hit_count'] = this.hitCount;
    if (this.fileHashesCommaOmitempty != null) {
      json[r'file_hashes,omitempty'] = this.fileHashesCommaOmitempty;
    } else {
      json[r'file_hashes,omitempty'] = null;
    }
      json[r'source'] = this.source_;
    return json;
  }

  /// Returns a new [CacheInspectResult] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static CacheInspectResult? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'prompt_hash'), 'Required key "CacheInspectResult[prompt_hash]" is missing from JSON.');
        assert(json[r'prompt_hash'] != null, 'Required key "CacheInspectResult[prompt_hash]" has a null value in JSON.');
        assert(json.containsKey(r'model_id'), 'Required key "CacheInspectResult[model_id]" is missing from JSON.');
        assert(json[r'model_id'] != null, 'Required key "CacheInspectResult[model_id]" has a null value in JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "CacheInspectResult[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "CacheInspectResult[created_at]" has a null value in JSON.');
        assert(json.containsKey(r'expires_at'), 'Required key "CacheInspectResult[expires_at]" is missing from JSON.');
        assert(json[r'expires_at'] != null, 'Required key "CacheInspectResult[expires_at]" has a null value in JSON.');
        assert(json.containsKey(r'hit_count'), 'Required key "CacheInspectResult[hit_count]" is missing from JSON.');
        assert(json[r'hit_count'] != null, 'Required key "CacheInspectResult[hit_count]" has a null value in JSON.');
        assert(json.containsKey(r'source'), 'Required key "CacheInspectResult[source]" is missing from JSON.');
        assert(json[r'source'] != null, 'Required key "CacheInspectResult[source]" has a null value in JSON.');
        return true;
      }());

      return CacheInspectResult(
        promptHash: mapValueOfType<String>(json, r'prompt_hash')!,
        modelId: mapValueOfType<String>(json, r'model_id')!,
        createdAt: mapValueOfType<String>(json, r'created_at')!,
        expiresAt: mapValueOfType<String>(json, r'expires_at')!,
        hitCount: mapValueOfType<int>(json, r'hit_count')!,
        fileHashesCommaOmitempty: mapValueOfType<String>(json, r'file_hashes,omitempty'),
        source_: mapValueOfType<String>(json, r'source')!,
      );
    }
    return null;
  }

  static List<CacheInspectResult> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <CacheInspectResult>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = CacheInspectResult.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, CacheInspectResult> mapFromJson(dynamic json) {
    final map = <String, CacheInspectResult>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = CacheInspectResult.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of CacheInspectResult-objects as value to a dart map
  static Map<String, List<CacheInspectResult>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<CacheInspectResult>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = CacheInspectResult.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'prompt_hash',
    'model_id',
    'created_at',
    'expires_at',
    'hit_count',
    'source',
  };
}

