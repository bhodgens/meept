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

class VectorStoreRequest {
  /// Returns a new [VectorStoreRequest] instance.
  VectorStoreRequest({
    required this.memoryId,
    required this.content,
    this.metadataCommaOmitempty,
  });

  String memoryId;

  String content;

  String? metadataCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is VectorStoreRequest &&
    other.memoryId == memoryId &&
    other.content == content &&
    other.metadataCommaOmitempty == metadataCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (memoryId.hashCode) +
    (content.hashCode) +
    (metadataCommaOmitempty == null ? 0 : metadataCommaOmitempty!.hashCode);

  @override
  String toString() => 'VectorStoreRequest[memoryId=$memoryId, content=$content, metadataCommaOmitempty=$metadataCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'memory_id'] = this.memoryId;
      json[r'content'] = this.content;
    if (this.metadataCommaOmitempty != null) {
      json[r'metadata,omitempty'] = this.metadataCommaOmitempty;
    } else {
      json[r'metadata,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [VectorStoreRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static VectorStoreRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'memory_id'), 'Required key "VectorStoreRequest[memory_id]" is missing from JSON.');
        assert(json[r'memory_id'] != null, 'Required key "VectorStoreRequest[memory_id]" has a null value in JSON.');
        assert(json.containsKey(r'content'), 'Required key "VectorStoreRequest[content]" is missing from JSON.');
        assert(json[r'content'] != null, 'Required key "VectorStoreRequest[content]" has a null value in JSON.');
        return true;
      }());

      return VectorStoreRequest(
        memoryId: mapValueOfType<String>(json, r'memory_id')!,
        content: mapValueOfType<String>(json, r'content')!,
        metadataCommaOmitempty: mapValueOfType<String>(json, r'metadata,omitempty'),
      );
    }
    return null;
  }

  static List<VectorStoreRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <VectorStoreRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = VectorStoreRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, VectorStoreRequest> mapFromJson(dynamic json) {
    final map = <String, VectorStoreRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = VectorStoreRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of VectorStoreRequest-objects as value to a dart map
  static Map<String, List<VectorStoreRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<VectorStoreRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = VectorStoreRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'memory_id',
    'content',
  };
}

