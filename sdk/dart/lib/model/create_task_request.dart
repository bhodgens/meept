//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class CreateTaskRequest {
  /// Returns a new [CreateTaskRequest] instance.
  CreateTaskRequest({
    required this.name,
    this.descriptionCommaOmitempty,
    this.sessionIdCommaOmitempty,
  });

  String name;

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
  String? sessionIdCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is CreateTaskRequest &&
    other.name == name &&
    other.descriptionCommaOmitempty == descriptionCommaOmitempty &&
    other.sessionIdCommaOmitempty == sessionIdCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (name.hashCode) +
    (descriptionCommaOmitempty == null ? 0 : descriptionCommaOmitempty!.hashCode) +
    (sessionIdCommaOmitempty == null ? 0 : sessionIdCommaOmitempty!.hashCode);

  @override
  String toString() => 'CreateTaskRequest[name=$name, descriptionCommaOmitempty=$descriptionCommaOmitempty, sessionIdCommaOmitempty=$sessionIdCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'name'] = this.name;
    if (this.descriptionCommaOmitempty != null) {
      json[r'description,omitempty'] = this.descriptionCommaOmitempty;
    } else {
      json[r'description,omitempty'] = null;
    }
    if (this.sessionIdCommaOmitempty != null) {
      json[r'session_id,omitempty'] = this.sessionIdCommaOmitempty;
    } else {
      json[r'session_id,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [CreateTaskRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static CreateTaskRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'name'), 'Required key "CreateTaskRequest[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "CreateTaskRequest[name]" has a null value in JSON.');
        return true;
      }());

      return CreateTaskRequest(
        name: mapValueOfType<String>(json, r'name')!,
        descriptionCommaOmitempty: mapValueOfType<String>(json, r'description,omitempty'),
        sessionIdCommaOmitempty: mapValueOfType<String>(json, r'session_id,omitempty'),
      );
    }
    return null;
  }

  static List<CreateTaskRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <CreateTaskRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = CreateTaskRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, CreateTaskRequest> mapFromJson(dynamic json) {
    final map = <String, CreateTaskRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = CreateTaskRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of CreateTaskRequest-objects as value to a dart map
  static Map<String, List<CreateTaskRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<CreateTaskRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = CreateTaskRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'name',
  };
}

