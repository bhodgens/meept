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

class CheckResponse {
  /// Returns a new [CheckResponse] instance.
  CheckResponse({
    required this.allowed,
    this.reasonCommaOmitempty,
  });

  bool allowed;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? reasonCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is CheckResponse &&
    other.allowed == allowed &&
    other.reasonCommaOmitempty == reasonCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (allowed.hashCode) +
    (reasonCommaOmitempty == null ? 0 : reasonCommaOmitempty!.hashCode);

  @override
  String toString() => 'CheckResponse[allowed=$allowed, reasonCommaOmitempty=$reasonCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'allowed'] = this.allowed;
    if (this.reasonCommaOmitempty != null) {
      json[r'reason,omitempty'] = this.reasonCommaOmitempty;
    } else {
      json[r'reason,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [CheckResponse] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static CheckResponse? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'allowed'), 'Required key "CheckResponse[allowed]" is missing from JSON.');
        assert(json[r'allowed'] != null, 'Required key "CheckResponse[allowed]" has a null value in JSON.');
        return true;
      }());

      return CheckResponse(
        allowed: mapValueOfType<bool>(json, r'allowed')!,
        reasonCommaOmitempty: mapValueOfType<String>(json, r'reason,omitempty'),
      );
    }
    return null;
  }

  static List<CheckResponse> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <CheckResponse>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = CheckResponse.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, CheckResponse> mapFromJson(dynamic json) {
    final map = <String, CheckResponse>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = CheckResponse.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of CheckResponse-objects as value to a dart map
  static Map<String, List<CheckResponse>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<CheckResponse>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = CheckResponse.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'allowed',
  };
}

