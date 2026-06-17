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

class ExecuteRequest {
  /// Returns a new [ExecuteRequest] instance.
  ExecuteRequest({
    required this.slug,
    required this.prompt,
  });

  String slug;

  String prompt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ExecuteRequest &&
    other.slug == slug &&
    other.prompt == prompt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (slug.hashCode) +
    (prompt.hashCode);

  @override
  String toString() => 'ExecuteRequest[slug=$slug, prompt=$prompt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'slug'] = this.slug;
      json[r'prompt'] = this.prompt;
    return json;
  }

  /// Returns a new [ExecuteRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ExecuteRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'slug'), 'Required key "ExecuteRequest[slug]" is missing from JSON.');
        assert(json[r'slug'] != null, 'Required key "ExecuteRequest[slug]" has a null value in JSON.');
        assert(json.containsKey(r'prompt'), 'Required key "ExecuteRequest[prompt]" is missing from JSON.');
        assert(json[r'prompt'] != null, 'Required key "ExecuteRequest[prompt]" has a null value in JSON.');
        return true;
      }());

      return ExecuteRequest(
        slug: mapValueOfType<String>(json, r'slug')!,
        prompt: mapValueOfType<String>(json, r'prompt')!,
      );
    }
    return null;
  }

  static List<ExecuteRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ExecuteRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ExecuteRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ExecuteRequest> mapFromJson(dynamic json) {
    final map = <String, ExecuteRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ExecuteRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ExecuteRequest-objects as value to a dart map
  static Map<String, List<ExecuteRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ExecuteRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ExecuteRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'slug',
    'prompt',
  };
}

