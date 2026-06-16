//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class GetSessionRequest {
  /// Returns a new [GetSessionRequest] instance.
  GetSessionRequest({
    required this.id,
  });

  String id;

  @override
  bool operator ==(Object other) => identical(this, other) || other is GetSessionRequest &&
    other.id == id;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode);

  @override
  String toString() => 'GetSessionRequest[id=$id]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
    return json;
  }

  /// Returns a new [GetSessionRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static GetSessionRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "GetSessionRequest[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "GetSessionRequest[id]" has a null value in JSON.');
        return true;
      }());

      return GetSessionRequest(
        id: mapValueOfType<String>(json, r'id')!,
      );
    }
    return null;
  }

  static List<GetSessionRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <GetSessionRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = GetSessionRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, GetSessionRequest> mapFromJson(dynamic json) {
    final map = <String, GetSessionRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = GetSessionRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of GetSessionRequest-objects as value to a dart map
  static Map<String, List<GetSessionRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<GetSessionRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = GetSessionRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
  };
}

