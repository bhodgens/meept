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

class FailRequest {
  /// Returns a new [FailRequest] instance.
  FailRequest({
    required this.jobId,
    required this.error,
  });

  String jobId;

  String error;

  @override
  bool operator ==(Object other) => identical(this, other) || other is FailRequest &&
    other.jobId == jobId &&
    other.error == error;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (jobId.hashCode) +
    (error.hashCode);

  @override
  String toString() => 'FailRequest[jobId=$jobId, error=$error]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'job_id'] = this.jobId;
      json[r'error'] = this.error;
    return json;
  }

  /// Returns a new [FailRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static FailRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'job_id'), 'Required key "FailRequest[job_id]" is missing from JSON.');
        assert(json[r'job_id'] != null, 'Required key "FailRequest[job_id]" has a null value in JSON.');
        assert(json.containsKey(r'error'), 'Required key "FailRequest[error]" is missing from JSON.');
        assert(json[r'error'] != null, 'Required key "FailRequest[error]" has a null value in JSON.');
        return true;
      }());

      return FailRequest(
        jobId: mapValueOfType<String>(json, r'job_id')!,
        error: mapValueOfType<String>(json, r'error')!,
      );
    }
    return null;
  }

  static List<FailRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <FailRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = FailRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, FailRequest> mapFromJson(dynamic json) {
    final map = <String, FailRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = FailRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of FailRequest-objects as value to a dart map
  static Map<String, List<FailRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<FailRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = FailRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'job_id',
    'error',
  };
}

