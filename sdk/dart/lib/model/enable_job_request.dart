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

class EnableJobRequest {
  /// Returns a new [EnableJobRequest] instance.
  EnableJobRequest({
    required this.id,
    required this.enabled,
  });

  String id;

  bool enabled;

  @override
  bool operator ==(Object other) => identical(this, other) || other is EnableJobRequest &&
    other.id == id &&
    other.enabled == enabled;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (enabled.hashCode);

  @override
  String toString() => 'EnableJobRequest[id=$id, enabled=$enabled]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'enabled'] = this.enabled;
    return json;
  }

  /// Returns a new [EnableJobRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static EnableJobRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "EnableJobRequest[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "EnableJobRequest[id]" has a null value in JSON.');
        assert(json.containsKey(r'enabled'), 'Required key "EnableJobRequest[enabled]" is missing from JSON.');
        assert(json[r'enabled'] != null, 'Required key "EnableJobRequest[enabled]" has a null value in JSON.');
        return true;
      }());

      return EnableJobRequest(
        id: mapValueOfType<String>(json, r'id')!,
        enabled: mapValueOfType<bool>(json, r'enabled')!,
      );
    }
    return null;
  }

  static List<EnableJobRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <EnableJobRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = EnableJobRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, EnableJobRequest> mapFromJson(dynamic json) {
    final map = <String, EnableJobRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = EnableJobRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of EnableJobRequest-objects as value to a dart map
  static Map<String, List<EnableJobRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<EnableJobRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = EnableJobRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'enabled',
  };
}

