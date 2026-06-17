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

class GenerateImprovementRequest {
  /// Returns a new [GenerateImprovementRequest] instance.
  GenerateImprovementRequest({
    required this.improvementId,
  });

  String improvementId;

  @override
  bool operator ==(Object other) => identical(this, other) || other is GenerateImprovementRequest &&
    other.improvementId == improvementId;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (improvementId.hashCode);

  @override
  String toString() => 'GenerateImprovementRequest[improvementId=$improvementId]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'improvement_id'] = this.improvementId;
    return json;
  }

  /// Returns a new [GenerateImprovementRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static GenerateImprovementRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'improvement_id'), 'Required key "GenerateImprovementRequest[improvement_id]" is missing from JSON.');
        assert(json[r'improvement_id'] != null, 'Required key "GenerateImprovementRequest[improvement_id]" has a null value in JSON.');
        return true;
      }());

      return GenerateImprovementRequest(
        improvementId: mapValueOfType<String>(json, r'improvement_id')!,
      );
    }
    return null;
  }

  static List<GenerateImprovementRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <GenerateImprovementRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = GenerateImprovementRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, GenerateImprovementRequest> mapFromJson(dynamic json) {
    final map = <String, GenerateImprovementRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = GenerateImprovementRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of GenerateImprovementRequest-objects as value to a dart map
  static Map<String, List<GenerateImprovementRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<GenerateImprovementRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = GenerateImprovementRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'improvement_id',
  };
}

