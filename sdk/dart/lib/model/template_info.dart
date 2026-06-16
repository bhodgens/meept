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

class TemplateInfo {
  /// Returns a new [TemplateInfo] instance.
  TemplateInfo({
    required this.name,
    required this.description,
    required this.scope,
    this.pathCommaOmitempty,
    required this.priority,
    this.bodyCommaOmitempty,
  });

  String name;

  String description;

  Object scope;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? pathCommaOmitempty;

  int priority;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? bodyCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is TemplateInfo &&
    other.name == name &&
    other.description == description &&
    other.scope == scope &&
    other.pathCommaOmitempty == pathCommaOmitempty &&
    other.priority == priority &&
    other.bodyCommaOmitempty == bodyCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (name.hashCode) +
    (description.hashCode) +
    (scope.hashCode) +
    (pathCommaOmitempty == null ? 0 : pathCommaOmitempty!.hashCode) +
    (priority.hashCode) +
    (bodyCommaOmitempty == null ? 0 : bodyCommaOmitempty!.hashCode);

  @override
  String toString() => 'TemplateInfo[name=$name, description=$description, scope=$scope, pathCommaOmitempty=$pathCommaOmitempty, priority=$priority, bodyCommaOmitempty=$bodyCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'name'] = this.name;
      json[r'description'] = this.description;
      json[r'scope'] = this.scope;
    if (this.pathCommaOmitempty != null) {
      json[r'path,omitempty'] = this.pathCommaOmitempty;
    } else {
      json[r'path,omitempty'] = null;
    }
      json[r'priority'] = this.priority;
    if (this.bodyCommaOmitempty != null) {
      json[r'body,omitempty'] = this.bodyCommaOmitempty;
    } else {
      json[r'body,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [TemplateInfo] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static TemplateInfo? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'name'), 'Required key "TemplateInfo[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "TemplateInfo[name]" has a null value in JSON.');
        assert(json.containsKey(r'description'), 'Required key "TemplateInfo[description]" is missing from JSON.');
        assert(json[r'description'] != null, 'Required key "TemplateInfo[description]" has a null value in JSON.');
        assert(json.containsKey(r'scope'), 'Required key "TemplateInfo[scope]" is missing from JSON.');
        assert(json[r'scope'] != null, 'Required key "TemplateInfo[scope]" has a null value in JSON.');
        assert(json.containsKey(r'priority'), 'Required key "TemplateInfo[priority]" is missing from JSON.');
        assert(json[r'priority'] != null, 'Required key "TemplateInfo[priority]" has a null value in JSON.');
        return true;
      }());

      return TemplateInfo(
        name: mapValueOfType<String>(json, r'name')!,
        description: mapValueOfType<String>(json, r'description')!,
        scope: mapValueOfType<Object>(json, r'scope')!,
        pathCommaOmitempty: mapValueOfType<String>(json, r'path,omitempty'),
        priority: mapValueOfType<int>(json, r'priority')!,
        bodyCommaOmitempty: mapValueOfType<String>(json, r'body,omitempty'),
      );
    }
    return null;
  }

  static List<TemplateInfo> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TemplateInfo>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TemplateInfo.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, TemplateInfo> mapFromJson(dynamic json) {
    final map = <String, TemplateInfo>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = TemplateInfo.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of TemplateInfo-objects as value to a dart map
  static Map<String, List<TemplateInfo>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<TemplateInfo>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = TemplateInfo.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'name',
    'description',
    'scope',
    'priority',
  };
}

