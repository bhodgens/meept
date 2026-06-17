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

class TemplatesGetRequest {
  /// Returns a new [TemplatesGetRequest] instance.
  TemplatesGetRequest({
    required this.name,
  });

  String name;

  @override
  bool operator ==(Object other) => identical(this, other) || other is TemplatesGetRequest &&
    other.name == name;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (name.hashCode);

  @override
  String toString() => 'TemplatesGetRequest[name=$name]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'name'] = this.name;
    return json;
  }

  /// Returns a new [TemplatesGetRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static TemplatesGetRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'name'), 'Required key "TemplatesGetRequest[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "TemplatesGetRequest[name]" has a null value in JSON.');
        return true;
      }());

      return TemplatesGetRequest(
        name: mapValueOfType<String>(json, r'name')!,
      );
    }
    return null;
  }

  static List<TemplatesGetRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TemplatesGetRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TemplatesGetRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, TemplatesGetRequest> mapFromJson(dynamic json) {
    final map = <String, TemplatesGetRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = TemplatesGetRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of TemplatesGetRequest-objects as value to a dart map
  static Map<String, List<TemplatesGetRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<TemplatesGetRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = TemplatesGetRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'name',
  };
}

