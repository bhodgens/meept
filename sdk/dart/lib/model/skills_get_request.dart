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

class SkillsGetRequest {
  /// Returns a new [SkillsGetRequest] instance.
  SkillsGetRequest({
    required this.slug,
  });

  String slug;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SkillsGetRequest &&
    other.slug == slug;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (slug.hashCode);

  @override
  String toString() => 'SkillsGetRequest[slug=$slug]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'slug'] = this.slug;
    return json;
  }

  /// Returns a new [SkillsGetRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SkillsGetRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'slug'), 'Required key "SkillsGetRequest[slug]" is missing from JSON.');
        assert(json[r'slug'] != null, 'Required key "SkillsGetRequest[slug]" has a null value in JSON.');
        return true;
      }());

      return SkillsGetRequest(
        slug: mapValueOfType<String>(json, r'slug')!,
      );
    }
    return null;
  }

  static List<SkillsGetRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SkillsGetRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SkillsGetRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SkillsGetRequest> mapFromJson(dynamic json) {
    final map = <String, SkillsGetRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SkillsGetRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SkillsGetRequest-objects as value to a dart map
  static Map<String, List<SkillsGetRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SkillsGetRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SkillsGetRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'slug',
  };
}

