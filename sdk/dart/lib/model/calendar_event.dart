//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class CalendarEvent {
  /// Returns a new [CalendarEvent] instance.
  CalendarEvent({
    required this.id,
    required this.summary,
    this.descriptionCommaOmitempty,
    this.locationCommaOmitempty,
    required this.start,
    required this.end,
    required this.allDay,
    this.statusCommaOmitempty,
    this.htmlLinkCommaOmitempty,
    this.attendeesCommaOmitempty = const [],
  });

  String id;

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

  bool allDay;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? statusCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? htmlLinkCommaOmitempty;

  List<String>? attendeesCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is CalendarEvent &&
    other.id == id &&
    other.summary == summary &&
    other.descriptionCommaOmitempty == descriptionCommaOmitempty &&
    other.locationCommaOmitempty == locationCommaOmitempty &&
    other.start == start &&
    other.end == end &&
    other.allDay == allDay &&
    other.statusCommaOmitempty == statusCommaOmitempty &&
    other.htmlLinkCommaOmitempty == htmlLinkCommaOmitempty &&
    _deepEquality.equals(other.attendeesCommaOmitempty, attendeesCommaOmitempty);

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (summary.hashCode) +
    (descriptionCommaOmitempty == null ? 0 : descriptionCommaOmitempty!.hashCode) +
    (locationCommaOmitempty == null ? 0 : locationCommaOmitempty!.hashCode) +
    (start.hashCode) +
    (end.hashCode) +
    (allDay.hashCode) +
    (statusCommaOmitempty == null ? 0 : statusCommaOmitempty!.hashCode) +
    (htmlLinkCommaOmitempty == null ? 0 : htmlLinkCommaOmitempty!.hashCode) +
    (attendeesCommaOmitempty == null ? 0 : attendeesCommaOmitempty!.hashCode);

  @override
  String toString() => 'CalendarEvent[id=$id, summary=$summary, descriptionCommaOmitempty=$descriptionCommaOmitempty, locationCommaOmitempty=$locationCommaOmitempty, start=$start, end=$end, allDay=$allDay, statusCommaOmitempty=$statusCommaOmitempty, htmlLinkCommaOmitempty=$htmlLinkCommaOmitempty, attendeesCommaOmitempty=$attendeesCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
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
      json[r'all_day'] = this.allDay;
    if (this.statusCommaOmitempty != null) {
      json[r'status,omitempty'] = this.statusCommaOmitempty;
    } else {
      json[r'status,omitempty'] = null;
    }
    if (this.htmlLinkCommaOmitempty != null) {
      json[r'html_link,omitempty'] = this.htmlLinkCommaOmitempty;
    } else {
      json[r'html_link,omitempty'] = null;
    }
    if (this.attendeesCommaOmitempty != null) {
      json[r'attendees,omitempty'] = this.attendeesCommaOmitempty;
    } else {
      json[r'attendees,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [CalendarEvent] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static CalendarEvent? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "CalendarEvent[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "CalendarEvent[id]" has a null value in JSON.');
        assert(json.containsKey(r'summary'), 'Required key "CalendarEvent[summary]" is missing from JSON.');
        assert(json[r'summary'] != null, 'Required key "CalendarEvent[summary]" has a null value in JSON.');
        assert(json.containsKey(r'start'), 'Required key "CalendarEvent[start]" is missing from JSON.');
        assert(json[r'start'] != null, 'Required key "CalendarEvent[start]" has a null value in JSON.');
        assert(json.containsKey(r'end'), 'Required key "CalendarEvent[end]" is missing from JSON.');
        assert(json[r'end'] != null, 'Required key "CalendarEvent[end]" has a null value in JSON.');
        assert(json.containsKey(r'all_day'), 'Required key "CalendarEvent[all_day]" is missing from JSON.');
        assert(json[r'all_day'] != null, 'Required key "CalendarEvent[all_day]" has a null value in JSON.');
        return true;
      }());

      return CalendarEvent(
        id: mapValueOfType<String>(json, r'id')!,
        summary: mapValueOfType<String>(json, r'summary')!,
        descriptionCommaOmitempty: mapValueOfType<String>(json, r'description,omitempty'),
        locationCommaOmitempty: mapValueOfType<String>(json, r'location,omitempty'),
        start: mapValueOfType<String>(json, r'start')!,
        end: mapValueOfType<String>(json, r'end')!,
        allDay: mapValueOfType<bool>(json, r'all_day')!,
        statusCommaOmitempty: mapValueOfType<String>(json, r'status,omitempty'),
        htmlLinkCommaOmitempty: mapValueOfType<String>(json, r'html_link,omitempty'),
        attendeesCommaOmitempty: json[r'attendees,omitempty'] is Iterable
            ? (json[r'attendees,omitempty'] as Iterable).cast<String>().toList(growable: false)
            : const [],
      );
    }
    return null;
  }

  static List<CalendarEvent> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <CalendarEvent>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = CalendarEvent.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, CalendarEvent> mapFromJson(dynamic json) {
    final map = <String, CalendarEvent>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = CalendarEvent.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of CalendarEvent-objects as value to a dart map
  static Map<String, List<CalendarEvent>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<CalendarEvent>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = CalendarEvent.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'summary',
    'start',
    'end',
    'all_day',
  };
}

