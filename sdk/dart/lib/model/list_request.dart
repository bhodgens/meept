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

class ListRequest {
  /// Returns a new [ListRequest] instance.
  ListRequest({
    this.stateCommaOmitempty,
    this.limitCommaOmitempty,
  });

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? stateCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? limitCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ListRequest &&
    other.stateCommaOmitempty == stateCommaOmitempty &&
    other.limitCommaOmitempty == limitCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (stateCommaOmitempty == null ? 0 : stateCommaOmitempty!.hashCode) +
    (limitCommaOmitempty == null ? 0 : limitCommaOmitempty!.hashCode);

  @override
  String toString() => 'ListRequest[stateCommaOmitempty=$stateCommaOmitempty, limitCommaOmitempty=$limitCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.stateCommaOmitempty != null) {
      json[r'state,omitempty'] = this.stateCommaOmitempty;
    } else {
      json[r'state,omitempty'] = null;
    }
    if (this.limitCommaOmitempty != null) {
      json[r'limit,omitempty'] = this.limitCommaOmitempty;
    } else {
      json[r'limit,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [ListRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ListRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return ListRequest(
        stateCommaOmitempty: mapValueOfType<String>(json, r'state,omitempty'),
        limitCommaOmitempty: mapValueOfType<int>(json, r'limit,omitempty'),
      );
    }
    return null;
  }

  static List<ListRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ListRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ListRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ListRequest> mapFromJson(dynamic json) {
    final map = <String, ListRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ListRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ListRequest-objects as value to a dart map
  static Map<String, List<ListRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ListRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ListRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

