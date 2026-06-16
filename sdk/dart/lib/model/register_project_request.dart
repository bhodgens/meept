//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class RegisterProjectRequest {
  /// Returns a new [RegisterProjectRequest] instance.
  RegisterProjectRequest({
    this.idCommaOmitempty,
    required this.name,
    this.gitUrlCommaOmitempty,
    this.localPathCommaOmitempty,
  });

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? idCommaOmitempty;

  String name;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? gitUrlCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? localPathCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is RegisterProjectRequest &&
    other.idCommaOmitempty == idCommaOmitempty &&
    other.name == name &&
    other.gitUrlCommaOmitempty == gitUrlCommaOmitempty &&
    other.localPathCommaOmitempty == localPathCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (idCommaOmitempty == null ? 0 : idCommaOmitempty!.hashCode) +
    (name.hashCode) +
    (gitUrlCommaOmitempty == null ? 0 : gitUrlCommaOmitempty!.hashCode) +
    (localPathCommaOmitempty == null ? 0 : localPathCommaOmitempty!.hashCode);

  @override
  String toString() => 'RegisterProjectRequest[idCommaOmitempty=$idCommaOmitempty, name=$name, gitUrlCommaOmitempty=$gitUrlCommaOmitempty, localPathCommaOmitempty=$localPathCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.idCommaOmitempty != null) {
      json[r'id,omitempty'] = this.idCommaOmitempty;
    } else {
      json[r'id,omitempty'] = null;
    }
      json[r'name'] = this.name;
    if (this.gitUrlCommaOmitempty != null) {
      json[r'git_url,omitempty'] = this.gitUrlCommaOmitempty;
    } else {
      json[r'git_url,omitempty'] = null;
    }
    if (this.localPathCommaOmitempty != null) {
      json[r'local_path,omitempty'] = this.localPathCommaOmitempty;
    } else {
      json[r'local_path,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [RegisterProjectRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static RegisterProjectRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'name'), 'Required key "RegisterProjectRequest[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "RegisterProjectRequest[name]" has a null value in JSON.');
        return true;
      }());

      return RegisterProjectRequest(
        idCommaOmitempty: mapValueOfType<String>(json, r'id,omitempty'),
        name: mapValueOfType<String>(json, r'name')!,
        gitUrlCommaOmitempty: mapValueOfType<String>(json, r'git_url,omitempty'),
        localPathCommaOmitempty: mapValueOfType<String>(json, r'local_path,omitempty'),
      );
    }
    return null;
  }

  static List<RegisterProjectRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RegisterProjectRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RegisterProjectRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, RegisterProjectRequest> mapFromJson(dynamic json) {
    final map = <String, RegisterProjectRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = RegisterProjectRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of RegisterProjectRequest-objects as value to a dart map
  static Map<String, List<RegisterProjectRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<RegisterProjectRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = RegisterProjectRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'name',
  };
}

