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

class PipelineInfo {
  /// Returns a new [PipelineInfo] instance.
  PipelineInfo({
    required this.id,
    required this.name,
    required this.status,
    required this.createdAt,
  });

  String id;

  String name;

  String status;

  String createdAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is PipelineInfo &&
    other.id == id &&
    other.name == name &&
    other.status == status &&
    other.createdAt == createdAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (name.hashCode) +
    (status.hashCode) +
    (createdAt.hashCode);

  @override
  String toString() => 'PipelineInfo[id=$id, name=$name, status=$status, createdAt=$createdAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'name'] = this.name;
      json[r'status'] = this.status;
      json[r'created_at'] = this.createdAt;
    return json;
  }

  /// Returns a new [PipelineInfo] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static PipelineInfo? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "PipelineInfo[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "PipelineInfo[id]" has a null value in JSON.');
        assert(json.containsKey(r'name'), 'Required key "PipelineInfo[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "PipelineInfo[name]" has a null value in JSON.');
        assert(json.containsKey(r'status'), 'Required key "PipelineInfo[status]" is missing from JSON.');
        assert(json[r'status'] != null, 'Required key "PipelineInfo[status]" has a null value in JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "PipelineInfo[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "PipelineInfo[created_at]" has a null value in JSON.');
        return true;
      }());

      return PipelineInfo(
        id: mapValueOfType<String>(json, r'id')!,
        name: mapValueOfType<String>(json, r'name')!,
        status: mapValueOfType<String>(json, r'status')!,
        createdAt: mapValueOfType<String>(json, r'created_at')!,
      );
    }
    return null;
  }

  static List<PipelineInfo> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <PipelineInfo>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = PipelineInfo.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, PipelineInfo> mapFromJson(dynamic json) {
    final map = <String, PipelineInfo>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = PipelineInfo.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of PipelineInfo-objects as value to a dart map
  static Map<String, List<PipelineInfo>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<PipelineInfo>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = PipelineInfo.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'name',
    'status',
    'created_at',
  };
}

