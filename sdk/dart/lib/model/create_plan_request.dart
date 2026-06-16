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

class CreatePlanRequest {
  /// Returns a new [CreatePlanRequest] instance.
  CreatePlanRequest({
    required this.title,
    this.descriptionCommaOmitempty,
    this.projectIdCommaOmitempty,
    this.projectPathCommaOmitempty,
    required this.sessionId,
  });

  String title;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? descriptionCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? projectIdCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? projectPathCommaOmitempty;

  String sessionId;

  @override
  bool operator ==(Object other) => identical(this, other) || other is CreatePlanRequest &&
    other.title == title &&
    other.descriptionCommaOmitempty == descriptionCommaOmitempty &&
    other.projectIdCommaOmitempty == projectIdCommaOmitempty &&
    other.projectPathCommaOmitempty == projectPathCommaOmitempty &&
    other.sessionId == sessionId;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (title.hashCode) +
    (descriptionCommaOmitempty == null ? 0 : descriptionCommaOmitempty!.hashCode) +
    (projectIdCommaOmitempty == null ? 0 : projectIdCommaOmitempty!.hashCode) +
    (projectPathCommaOmitempty == null ? 0 : projectPathCommaOmitempty!.hashCode) +
    (sessionId.hashCode);

  @override
  String toString() => 'CreatePlanRequest[title=$title, descriptionCommaOmitempty=$descriptionCommaOmitempty, projectIdCommaOmitempty=$projectIdCommaOmitempty, projectPathCommaOmitempty=$projectPathCommaOmitempty, sessionId=$sessionId]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'title'] = this.title;
    if (this.descriptionCommaOmitempty != null) {
      json[r'description,omitempty'] = this.descriptionCommaOmitempty;
    } else {
      json[r'description,omitempty'] = null;
    }
    if (this.projectIdCommaOmitempty != null) {
      json[r'project_id,omitempty'] = this.projectIdCommaOmitempty;
    } else {
      json[r'project_id,omitempty'] = null;
    }
    if (this.projectPathCommaOmitempty != null) {
      json[r'project_path,omitempty'] = this.projectPathCommaOmitempty;
    } else {
      json[r'project_path,omitempty'] = null;
    }
      json[r'session_id'] = this.sessionId;
    return json;
  }

  /// Returns a new [CreatePlanRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static CreatePlanRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'title'), 'Required key "CreatePlanRequest[title]" is missing from JSON.');
        assert(json[r'title'] != null, 'Required key "CreatePlanRequest[title]" has a null value in JSON.');
        assert(json.containsKey(r'session_id'), 'Required key "CreatePlanRequest[session_id]" is missing from JSON.');
        assert(json[r'session_id'] != null, 'Required key "CreatePlanRequest[session_id]" has a null value in JSON.');
        return true;
      }());

      return CreatePlanRequest(
        title: mapValueOfType<String>(json, r'title')!,
        descriptionCommaOmitempty: mapValueOfType<String>(json, r'description,omitempty'),
        projectIdCommaOmitempty: mapValueOfType<String>(json, r'project_id,omitempty'),
        projectPathCommaOmitempty: mapValueOfType<String>(json, r'project_path,omitempty'),
        sessionId: mapValueOfType<String>(json, r'session_id')!,
      );
    }
    return null;
  }

  static List<CreatePlanRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <CreatePlanRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = CreatePlanRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, CreatePlanRequest> mapFromJson(dynamic json) {
    final map = <String, CreatePlanRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = CreatePlanRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of CreatePlanRequest-objects as value to a dart map
  static Map<String, List<CreatePlanRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<CreatePlanRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = CreatePlanRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'title',
    'session_id',
  };
}

