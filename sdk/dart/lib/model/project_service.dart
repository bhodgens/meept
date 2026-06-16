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

class ProjectService {
  /// Returns a new [ProjectService] instance.
  ProjectService({
    this.pm,
    this.store,
  });

  Object? pm;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? store;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ProjectService &&
    other.pm == pm &&
    other.store == store;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (pm == null ? 0 : pm!.hashCode) +
    (store == null ? 0 : store!.hashCode);

  @override
  String toString() => 'ProjectService[pm=$pm, store=$store]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.pm != null) {
      json[r'pm'] = this.pm;
    } else {
      json[r'pm'] = null;
    }
    if (this.store != null) {
      json[r'store'] = this.store;
    } else {
      json[r'store'] = null;
    }
    return json;
  }

  /// Returns a new [ProjectService] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ProjectService? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return ProjectService(
        pm: mapValueOfType<Object>(json, r'pm'),
        store: mapValueOfType<Object>(json, r'store'),
      );
    }
    return null;
  }

  static List<ProjectService> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ProjectService>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ProjectService.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ProjectService> mapFromJson(dynamic json) {
    final map = <String, ProjectService>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ProjectService.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ProjectService-objects as value to a dart map
  static Map<String, List<ProjectService>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ProjectService>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ProjectService.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

