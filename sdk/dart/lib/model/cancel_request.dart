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

class CancelRequest {
  /// Returns a new [CancelRequest] instance.
  CancelRequest({
    required this.cycleId,
  });

  String cycleId;

  @override
  bool operator ==(Object other) => identical(this, other) || other is CancelRequest &&
    other.cycleId == cycleId;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (cycleId.hashCode);

  @override
  String toString() => 'CancelRequest[cycleId=$cycleId]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'cycle_id'] = this.cycleId;
    return json;
  }

  /// Returns a new [CancelRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static CancelRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'cycle_id'), 'Required key "CancelRequest[cycle_id]" is missing from JSON.');
        assert(json[r'cycle_id'] != null, 'Required key "CancelRequest[cycle_id]" has a null value in JSON.');
        return true;
      }());

      return CancelRequest(
        cycleId: mapValueOfType<String>(json, r'cycle_id')!,
      );
    }
    return null;
  }

  static List<CancelRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <CancelRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = CancelRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, CancelRequest> mapFromJson(dynamic json) {
    final map = <String, CancelRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = CancelRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of CancelRequest-objects as value to a dart map
  static Map<String, List<CancelRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<CancelRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = CancelRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'cycle_id',
  };
}

