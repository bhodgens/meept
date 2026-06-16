//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class AttendeeInfo {
  /// Returns a new [AttendeeInfo] instance.
  AttendeeInfo({
    required this.email,
    this.displayNameCommaOmitempty,
    this.responseCommaOmitempty,
  });

  String email;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? displayNameCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? responseCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is AttendeeInfo &&
    other.email == email &&
    other.displayNameCommaOmitempty == displayNameCommaOmitempty &&
    other.responseCommaOmitempty == responseCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (email.hashCode) +
    (displayNameCommaOmitempty == null ? 0 : displayNameCommaOmitempty!.hashCode) +
    (responseCommaOmitempty == null ? 0 : responseCommaOmitempty!.hashCode);

  @override
  String toString() => 'AttendeeInfo[email=$email, displayNameCommaOmitempty=$displayNameCommaOmitempty, responseCommaOmitempty=$responseCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'email'] = this.email;
    if (this.displayNameCommaOmitempty != null) {
      json[r'display_name,omitempty'] = this.displayNameCommaOmitempty;
    } else {
      json[r'display_name,omitempty'] = null;
    }
    if (this.responseCommaOmitempty != null) {
      json[r'response,omitempty'] = this.responseCommaOmitempty;
    } else {
      json[r'response,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [AttendeeInfo] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static AttendeeInfo? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'email'), 'Required key "AttendeeInfo[email]" is missing from JSON.');
        assert(json[r'email'] != null, 'Required key "AttendeeInfo[email]" has a null value in JSON.');
        return true;
      }());

      return AttendeeInfo(
        email: mapValueOfType<String>(json, r'email')!,
        displayNameCommaOmitempty: mapValueOfType<String>(json, r'display_name,omitempty'),
        responseCommaOmitempty: mapValueOfType<String>(json, r'response,omitempty'),
      );
    }
    return null;
  }

  static List<AttendeeInfo> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <AttendeeInfo>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = AttendeeInfo.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, AttendeeInfo> mapFromJson(dynamic json) {
    final map = <String, AttendeeInfo>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = AttendeeInfo.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of AttendeeInfo-objects as value to a dart map
  static Map<String, List<AttendeeInfo>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<AttendeeInfo>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = AttendeeInfo.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'email',
  };
}

