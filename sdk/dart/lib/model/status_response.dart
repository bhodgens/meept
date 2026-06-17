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

class StatusResponse {
  /// Returns a new [StatusResponse] instance.
  StatusResponse({
    required this.enabled,
    this.lastCycleCommaOmitempty,
    required this.skillsLearned,
    required this.pendingTasks,
  });

  bool enabled;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? lastCycleCommaOmitempty;

  int skillsLearned;

  int pendingTasks;

  @override
  bool operator ==(Object other) => identical(this, other) || other is StatusResponse &&
    other.enabled == enabled &&
    other.lastCycleCommaOmitempty == lastCycleCommaOmitempty &&
    other.skillsLearned == skillsLearned &&
    other.pendingTasks == pendingTasks;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (enabled.hashCode) +
    (lastCycleCommaOmitempty == null ? 0 : lastCycleCommaOmitempty!.hashCode) +
    (skillsLearned.hashCode) +
    (pendingTasks.hashCode);

  @override
  String toString() => 'StatusResponse[enabled=$enabled, lastCycleCommaOmitempty=$lastCycleCommaOmitempty, skillsLearned=$skillsLearned, pendingTasks=$pendingTasks]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'enabled'] = this.enabled;
    if (this.lastCycleCommaOmitempty != null) {
      json[r'last_cycle,omitempty'] = this.lastCycleCommaOmitempty;
    } else {
      json[r'last_cycle,omitempty'] = null;
    }
      json[r'skills_learned'] = this.skillsLearned;
      json[r'pending_tasks'] = this.pendingTasks;
    return json;
  }

  /// Returns a new [StatusResponse] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static StatusResponse? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'enabled'), 'Required key "StatusResponse[enabled]" is missing from JSON.');
        assert(json[r'enabled'] != null, 'Required key "StatusResponse[enabled]" has a null value in JSON.');
        assert(json.containsKey(r'skills_learned'), 'Required key "StatusResponse[skills_learned]" is missing from JSON.');
        assert(json[r'skills_learned'] != null, 'Required key "StatusResponse[skills_learned]" has a null value in JSON.');
        assert(json.containsKey(r'pending_tasks'), 'Required key "StatusResponse[pending_tasks]" is missing from JSON.');
        assert(json[r'pending_tasks'] != null, 'Required key "StatusResponse[pending_tasks]" has a null value in JSON.');
        return true;
      }());

      return StatusResponse(
        enabled: mapValueOfType<bool>(json, r'enabled')!,
        lastCycleCommaOmitempty: mapValueOfType<String>(json, r'last_cycle,omitempty'),
        skillsLearned: mapValueOfType<int>(json, r'skills_learned')!,
        pendingTasks: mapValueOfType<int>(json, r'pending_tasks')!,
      );
    }
    return null;
  }

  static List<StatusResponse> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <StatusResponse>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = StatusResponse.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, StatusResponse> mapFromJson(dynamic json) {
    final map = <String, StatusResponse>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = StatusResponse.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of StatusResponse-objects as value to a dart map
  static Map<String, List<StatusResponse>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<StatusResponse>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = StatusResponse.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'enabled',
    'skills_learned',
    'pending_tasks',
  };
}

