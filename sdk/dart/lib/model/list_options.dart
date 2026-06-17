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

class ListOptions {
  /// Returns a new [ListOptions] instance.
  ListOptions({
    this.limitCommaOmitempty,
    this.offsetCommaOmitempty,
    this.filterCommaOmitempty,
  });

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? limitCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? offsetCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? filterCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ListOptions &&
    other.limitCommaOmitempty == limitCommaOmitempty &&
    other.offsetCommaOmitempty == offsetCommaOmitempty &&
    other.filterCommaOmitempty == filterCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (limitCommaOmitempty == null ? 0 : limitCommaOmitempty!.hashCode) +
    (offsetCommaOmitempty == null ? 0 : offsetCommaOmitempty!.hashCode) +
    (filterCommaOmitempty == null ? 0 : filterCommaOmitempty!.hashCode);

  @override
  String toString() => 'ListOptions[limitCommaOmitempty=$limitCommaOmitempty, offsetCommaOmitempty=$offsetCommaOmitempty, filterCommaOmitempty=$filterCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.limitCommaOmitempty != null) {
      json[r'limit,omitempty'] = this.limitCommaOmitempty;
    } else {
      json[r'limit,omitempty'] = null;
    }
    if (this.offsetCommaOmitempty != null) {
      json[r'offset,omitempty'] = this.offsetCommaOmitempty;
    } else {
      json[r'offset,omitempty'] = null;
    }
    if (this.filterCommaOmitempty != null) {
      json[r'filter,omitempty'] = this.filterCommaOmitempty;
    } else {
      json[r'filter,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [ListOptions] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ListOptions? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return ListOptions(
        limitCommaOmitempty: mapValueOfType<int>(json, r'limit,omitempty'),
        offsetCommaOmitempty: mapValueOfType<int>(json, r'offset,omitempty'),
        filterCommaOmitempty: mapValueOfType<String>(json, r'filter,omitempty'),
      );
    }
    return null;
  }

  static List<ListOptions> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ListOptions>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ListOptions.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ListOptions> mapFromJson(dynamic json) {
    final map = <String, ListOptions>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ListOptions.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ListOptions-objects as value to a dart map
  static Map<String, List<ListOptions>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ListOptions>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ListOptions.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

