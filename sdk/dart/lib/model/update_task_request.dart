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

class UpdateTaskRequest {
  /// Returns a new [UpdateTaskRequest] instance.
  UpdateTaskRequest({
    required this.id,
    this.stateCommaOmitempty,
    this.nameCommaOmitempty,
  });

  String id;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? stateCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? nameCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is UpdateTaskRequest &&
    other.id == id &&
    other.stateCommaOmitempty == stateCommaOmitempty &&
    other.nameCommaOmitempty == nameCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (stateCommaOmitempty == null ? 0 : stateCommaOmitempty!.hashCode) +
    (nameCommaOmitempty == null ? 0 : nameCommaOmitempty!.hashCode);

  @override
  String toString() => 'UpdateTaskRequest[id=$id, stateCommaOmitempty=$stateCommaOmitempty, nameCommaOmitempty=$nameCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
    if (this.stateCommaOmitempty != null) {
      json[r'state,omitempty'] = this.stateCommaOmitempty;
    } else {
      json[r'state,omitempty'] = null;
    }
    if (this.nameCommaOmitempty != null) {
      json[r'name,omitempty'] = this.nameCommaOmitempty;
    } else {
      json[r'name,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [UpdateTaskRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static UpdateTaskRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "UpdateTaskRequest[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "UpdateTaskRequest[id]" has a null value in JSON.');
        return true;
      }());

      return UpdateTaskRequest(
        id: mapValueOfType<String>(json, r'id')!,
        stateCommaOmitempty: mapValueOfType<String>(json, r'state,omitempty'),
        nameCommaOmitempty: mapValueOfType<String>(json, r'name,omitempty'),
      );
    }
    return null;
  }

  static List<UpdateTaskRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <UpdateTaskRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = UpdateTaskRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, UpdateTaskRequest> mapFromJson(dynamic json) {
    final map = <String, UpdateTaskRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = UpdateTaskRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of UpdateTaskRequest-objects as value to a dart map
  static Map<String, List<UpdateTaskRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<UpdateTaskRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = UpdateTaskRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
  };
}

