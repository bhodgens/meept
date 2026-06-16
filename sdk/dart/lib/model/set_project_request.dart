//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class SetProjectRequest {
  /// Returns a new [SetProjectRequest] instance.
  SetProjectRequest({
    required this.sessionId,
    required this.projectId,
  });

  String sessionId;

  String projectId;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SetProjectRequest &&
    other.sessionId == sessionId &&
    other.projectId == projectId;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (sessionId.hashCode) +
    (projectId.hashCode);

  @override
  String toString() => 'SetProjectRequest[sessionId=$sessionId, projectId=$projectId]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'session_id'] = this.sessionId;
      json[r'project_id'] = this.projectId;
    return json;
  }

  /// Returns a new [SetProjectRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SetProjectRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'session_id'), 'Required key "SetProjectRequest[session_id]" is missing from JSON.');
        assert(json[r'session_id'] != null, 'Required key "SetProjectRequest[session_id]" has a null value in JSON.');
        assert(json.containsKey(r'project_id'), 'Required key "SetProjectRequest[project_id]" is missing from JSON.');
        assert(json[r'project_id'] != null, 'Required key "SetProjectRequest[project_id]" has a null value in JSON.');
        return true;
      }());

      return SetProjectRequest(
        sessionId: mapValueOfType<String>(json, r'session_id')!,
        projectId: mapValueOfType<String>(json, r'project_id')!,
      );
    }
    return null;
  }

  static List<SetProjectRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SetProjectRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SetProjectRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SetProjectRequest> mapFromJson(dynamic json) {
    final map = <String, SetProjectRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SetProjectRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SetProjectRequest-objects as value to a dart map
  static Map<String, List<SetProjectRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SetProjectRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SetProjectRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'session_id',
    'project_id',
  };
}

