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

class AddJobRequest {
  /// Returns a new [AddJobRequest] instance.
  AddJobRequest({
    required this.id,
    required this.name,
    required this.schedule,
    required this.type,
    this.agentConfigCommaOmitempty,
    this.shellConfigCommaOmitempty,
    this.enabledCommaOmitempty,
  });

  String id;

  String name;

  String schedule;

  String type;

  Object? agentConfigCommaOmitempty;

  Object? shellConfigCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  bool? enabledCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is AddJobRequest &&
    other.id == id &&
    other.name == name &&
    other.schedule == schedule &&
    other.type == type &&
    other.agentConfigCommaOmitempty == agentConfigCommaOmitempty &&
    other.shellConfigCommaOmitempty == shellConfigCommaOmitempty &&
    other.enabledCommaOmitempty == enabledCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (name.hashCode) +
    (schedule.hashCode) +
    (type.hashCode) +
    (agentConfigCommaOmitempty == null ? 0 : agentConfigCommaOmitempty!.hashCode) +
    (shellConfigCommaOmitempty == null ? 0 : shellConfigCommaOmitempty!.hashCode) +
    (enabledCommaOmitempty == null ? 0 : enabledCommaOmitempty!.hashCode);

  @override
  String toString() => 'AddJobRequest[id=$id, name=$name, schedule=$schedule, type=$type, agentConfigCommaOmitempty=$agentConfigCommaOmitempty, shellConfigCommaOmitempty=$shellConfigCommaOmitempty, enabledCommaOmitempty=$enabledCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'name'] = this.name;
      json[r'schedule'] = this.schedule;
      json[r'type'] = this.type;
    if (this.agentConfigCommaOmitempty != null) {
      json[r'agent_config,omitempty'] = this.agentConfigCommaOmitempty;
    } else {
      json[r'agent_config,omitempty'] = null;
    }
    if (this.shellConfigCommaOmitempty != null) {
      json[r'shell_config,omitempty'] = this.shellConfigCommaOmitempty;
    } else {
      json[r'shell_config,omitempty'] = null;
    }
    if (this.enabledCommaOmitempty != null) {
      json[r'enabled,omitempty'] = this.enabledCommaOmitempty;
    } else {
      json[r'enabled,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [AddJobRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static AddJobRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "AddJobRequest[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "AddJobRequest[id]" has a null value in JSON.');
        assert(json.containsKey(r'name'), 'Required key "AddJobRequest[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "AddJobRequest[name]" has a null value in JSON.');
        assert(json.containsKey(r'schedule'), 'Required key "AddJobRequest[schedule]" is missing from JSON.');
        assert(json[r'schedule'] != null, 'Required key "AddJobRequest[schedule]" has a null value in JSON.');
        assert(json.containsKey(r'type'), 'Required key "AddJobRequest[type]" is missing from JSON.');
        assert(json[r'type'] != null, 'Required key "AddJobRequest[type]" has a null value in JSON.');
        return true;
      }());

      return AddJobRequest(
        id: mapValueOfType<String>(json, r'id')!,
        name: mapValueOfType<String>(json, r'name')!,
        schedule: mapValueOfType<String>(json, r'schedule')!,
        type: mapValueOfType<String>(json, r'type')!,
        agentConfigCommaOmitempty: mapValueOfType<Object>(json, r'agent_config,omitempty'),
        shellConfigCommaOmitempty: mapValueOfType<Object>(json, r'shell_config,omitempty'),
        enabledCommaOmitempty: mapValueOfType<bool>(json, r'enabled,omitempty'),
      );
    }
    return null;
  }

  static List<AddJobRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <AddJobRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = AddJobRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, AddJobRequest> mapFromJson(dynamic json) {
    final map = <String, AddJobRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = AddJobRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of AddJobRequest-objects as value to a dart map
  static Map<String, List<AddJobRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<AddJobRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = AddJobRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'name',
    'schedule',
    'type',
  };
}

