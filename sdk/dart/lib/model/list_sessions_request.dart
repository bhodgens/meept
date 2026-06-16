//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ListSessionsRequest {
  /// Returns a new [ListSessionsRequest] instance.
  ListSessionsRequest({
    this.limitCommaOmitempty,
  });

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? limitCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ListSessionsRequest &&
    other.limitCommaOmitempty == limitCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (limitCommaOmitempty == null ? 0 : limitCommaOmitempty!.hashCode);

  @override
  String toString() => 'ListSessionsRequest[limitCommaOmitempty=$limitCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.limitCommaOmitempty != null) {
      json[r'limit,omitempty'] = this.limitCommaOmitempty;
    } else {
      json[r'limit,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [ListSessionsRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ListSessionsRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return ListSessionsRequest(
        limitCommaOmitempty: mapValueOfType<int>(json, r'limit,omitempty'),
      );
    }
    return null;
  }

  static List<ListSessionsRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ListSessionsRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ListSessionsRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ListSessionsRequest> mapFromJson(dynamic json) {
    final map = <String, ListSessionsRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ListSessionsRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ListSessionsRequest-objects as value to a dart map
  static Map<String, List<ListSessionsRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ListSessionsRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ListSessionsRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

