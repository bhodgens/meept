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

class RejectImprovementRequest {
  /// Returns a new [RejectImprovementRequest] instance.
  RejectImprovementRequest({
    required this.improvementId,
    required this.reason,
  });

  String improvementId;

  String reason;

  @override
  bool operator ==(Object other) => identical(this, other) || other is RejectImprovementRequest &&
    other.improvementId == improvementId &&
    other.reason == reason;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (improvementId.hashCode) +
    (reason.hashCode);

  @override
  String toString() => 'RejectImprovementRequest[improvementId=$improvementId, reason=$reason]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'improvement_id'] = this.improvementId;
      json[r'reason'] = this.reason;
    return json;
  }

  /// Returns a new [RejectImprovementRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static RejectImprovementRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'improvement_id'), 'Required key "RejectImprovementRequest[improvement_id]" is missing from JSON.');
        assert(json[r'improvement_id'] != null, 'Required key "RejectImprovementRequest[improvement_id]" has a null value in JSON.');
        assert(json.containsKey(r'reason'), 'Required key "RejectImprovementRequest[reason]" is missing from JSON.');
        assert(json[r'reason'] != null, 'Required key "RejectImprovementRequest[reason]" has a null value in JSON.');
        return true;
      }());

      return RejectImprovementRequest(
        improvementId: mapValueOfType<String>(json, r'improvement_id')!,
        reason: mapValueOfType<String>(json, r'reason')!,
      );
    }
    return null;
  }

  static List<RejectImprovementRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RejectImprovementRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RejectImprovementRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, RejectImprovementRequest> mapFromJson(dynamic json) {
    final map = <String, RejectImprovementRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = RejectImprovementRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of RejectImprovementRequest-objects as value to a dart map
  static Map<String, List<RejectImprovementRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<RejectImprovementRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = RejectImprovementRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'improvement_id',
    'reason',
  };
}

