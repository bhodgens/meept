//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class CreateEventRequest {
  /// Returns a new [CreateEventRequest] instance.
  CreateEventRequest({
    required this.summary,
    this.descriptionCommaOmitempty,
    this.locationCommaOmitempty,
    required this.start,
    required this.end,
    this.attendeesCommaOmitempty,
  });

  String summary;

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
  String? locationCommaOmitempty;

  String start;

  String end;

  String? attendeesCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is CreateEventRequest &&
    other.summary == summary &&
    other.descriptionCommaOmitempty == descriptionCommaOmitempty &&
    other.locationCommaOmitempty == locationCommaOmitempty &&
    other.start == start &&
    other.end == end &&
    other.attendeesCommaOmitempty == attendeesCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (summary.hashCode) +
    (descriptionCommaOmitempty == null ? 0 : descriptionCommaOmitempty!.hashCode) +
    (locationCommaOmitempty == null ? 0 : locationCommaOmitempty!.hashCode) +
    (start.hashCode) +
    (end.hashCode) +
    (attendeesCommaOmitempty == null ? 0 : attendeesCommaOmitempty!.hashCode);

  @override
  String toString() => 'CreateEventRequest[summary=$summary, descriptionCommaOmitempty=$descriptionCommaOmitempty, locationCommaOmitempty=$locationCommaOmitempty, start=$start, end=$end, attendeesCommaOmitempty=$attendeesCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'summary'] = this.summary;
    if (this.descriptionCommaOmitempty != null) {
      json[r'description,omitempty'] = this.descriptionCommaOmitempty;
    } else {
      json[r'description,omitempty'] = null;
    }
    if (this.locationCommaOmitempty != null) {
      json[r'location,omitempty'] = this.locationCommaOmitempty;
    } else {
      json[r'location,omitempty'] = null;
    }
      json[r'start'] = this.start;
      json[r'end'] = this.end;
    if (this.attendeesCommaOmitempty != null) {
      json[r'attendees,omitempty'] = this.attendeesCommaOmitempty;
    } else {
      json[r'attendees,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [CreateEventRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static CreateEventRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'summary'), 'Required key "CreateEventRequest[summary]" is missing from JSON.');
        assert(json[r'summary'] != null, 'Required key "CreateEventRequest[summary]" has a null value in JSON.');
        assert(json.containsKey(r'start'), 'Required key "CreateEventRequest[start]" is missing from JSON.');
        assert(json[r'start'] != null, 'Required key "CreateEventRequest[start]" has a null value in JSON.');
        assert(json.containsKey(r'end'), 'Required key "CreateEventRequest[end]" is missing from JSON.');
        assert(json[r'end'] != null, 'Required key "CreateEventRequest[end]" has a null value in JSON.');
        return true;
      }());

      return CreateEventRequest(
        summary: mapValueOfType<String>(json, r'summary')!,
        descriptionCommaOmitempty: mapValueOfType<String>(json, r'description,omitempty'),
        locationCommaOmitempty: mapValueOfType<String>(json, r'location,omitempty'),
        start: mapValueOfType<String>(json, r'start')!,
        end: mapValueOfType<String>(json, r'end')!,
        attendeesCommaOmitempty: mapValueOfType<String>(json, r'attendees,omitempty'),
      );
    }
    return null;
  }

  static List<CreateEventRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <CreateEventRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = CreateEventRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, CreateEventRequest> mapFromJson(dynamic json) {
    final map = <String, CreateEventRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = CreateEventRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of CreateEventRequest-objects as value to a dart map
  static Map<String, List<CreateEventRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<CreateEventRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = CreateEventRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'summary',
    'start',
    'end',
  };
}

