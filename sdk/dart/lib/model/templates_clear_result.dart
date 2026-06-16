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

class TemplatesClearResult {
  /// Returns a new [TemplatesClearResult] instance.
  TemplatesClearResult({
    required this.cleared,
  });

  String? cleared;

  @override
  bool operator ==(Object other) => identical(this, other) || other is TemplatesClearResult &&
    other.cleared == cleared;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (cleared == null ? 0 : cleared!.hashCode);

  @override
  String toString() => 'TemplatesClearResult[cleared=$cleared]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.cleared != null) {
      json[r'cleared'] = this.cleared;
    } else {
      json[r'cleared'] = null;
    }
    return json;
  }

  /// Returns a new [TemplatesClearResult] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static TemplatesClearResult? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'cleared'), 'Required key "TemplatesClearResult[cleared]" is missing from JSON.');
        return true;
      }());

      return TemplatesClearResult(
        cleared: mapValueOfType<String>(json, r'cleared'),
      );
    }
    return null;
  }

  static List<TemplatesClearResult> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TemplatesClearResult>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TemplatesClearResult.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, TemplatesClearResult> mapFromJson(dynamic json) {
    final map = <String, TemplatesClearResult>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = TemplatesClearResult.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of TemplatesClearResult-objects as value to a dart map
  static Map<String, List<TemplatesClearResult>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<TemplatesClearResult>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = TemplatesClearResult.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'cleared',
  };
}

