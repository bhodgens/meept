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

class PipelineStepStatus {
  /// Returns a new [PipelineStepStatus] instance.
  PipelineStepStatus({
    required this.id,
    required this.name,
    required this.status,
    this.errorCommaOmitempty,
    this.startedAtCommaOmitempty,
    this.endedAtCommaOmitempty,
  });

  String id;

  String name;

  String status;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? errorCommaOmitempty;

  String? startedAtCommaOmitempty;

  String? endedAtCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is PipelineStepStatus &&
    other.id == id &&
    other.name == name &&
    other.status == status &&
    other.errorCommaOmitempty == errorCommaOmitempty &&
    other.startedAtCommaOmitempty == startedAtCommaOmitempty &&
    other.endedAtCommaOmitempty == endedAtCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (name.hashCode) +
    (status.hashCode) +
    (errorCommaOmitempty == null ? 0 : errorCommaOmitempty!.hashCode) +
    (startedAtCommaOmitempty == null ? 0 : startedAtCommaOmitempty!.hashCode) +
    (endedAtCommaOmitempty == null ? 0 : endedAtCommaOmitempty!.hashCode);

  @override
  String toString() => 'PipelineStepStatus[id=$id, name=$name, status=$status, errorCommaOmitempty=$errorCommaOmitempty, startedAtCommaOmitempty=$startedAtCommaOmitempty, endedAtCommaOmitempty=$endedAtCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'name'] = this.name;
      json[r'status'] = this.status;
    if (this.errorCommaOmitempty != null) {
      json[r'error,omitempty'] = this.errorCommaOmitempty;
    } else {
      json[r'error,omitempty'] = null;
    }
    if (this.startedAtCommaOmitempty != null) {
      json[r'started_at,omitempty'] = this.startedAtCommaOmitempty;
    } else {
      json[r'started_at,omitempty'] = null;
    }
    if (this.endedAtCommaOmitempty != null) {
      json[r'ended_at,omitempty'] = this.endedAtCommaOmitempty;
    } else {
      json[r'ended_at,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [PipelineStepStatus] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static PipelineStepStatus? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "PipelineStepStatus[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "PipelineStepStatus[id]" has a null value in JSON.');
        assert(json.containsKey(r'name'), 'Required key "PipelineStepStatus[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "PipelineStepStatus[name]" has a null value in JSON.');
        assert(json.containsKey(r'status'), 'Required key "PipelineStepStatus[status]" is missing from JSON.');
        assert(json[r'status'] != null, 'Required key "PipelineStepStatus[status]" has a null value in JSON.');
        return true;
      }());

      return PipelineStepStatus(
        id: mapValueOfType<String>(json, r'id')!,
        name: mapValueOfType<String>(json, r'name')!,
        status: mapValueOfType<String>(json, r'status')!,
        errorCommaOmitempty: mapValueOfType<String>(json, r'error,omitempty'),
        startedAtCommaOmitempty: mapValueOfType<String>(json, r'started_at,omitempty'),
        endedAtCommaOmitempty: mapValueOfType<String>(json, r'ended_at,omitempty'),
      );
    }
    return null;
  }

  static List<PipelineStepStatus> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <PipelineStepStatus>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = PipelineStepStatus.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, PipelineStepStatus> mapFromJson(dynamic json) {
    final map = <String, PipelineStepStatus>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = PipelineStepStatus.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of PipelineStepStatus-objects as value to a dart map
  static Map<String, List<PipelineStepStatus>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<PipelineStepStatus>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = PipelineStepStatus.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'name',
    'status',
  };
}

