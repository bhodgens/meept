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

class UpdateEventRequest {
  /// Returns a new [UpdateEventRequest] instance.
  UpdateEventRequest({
    required this.id,
    this.summaryCommaOmitempty,
    this.descriptionCommaOmitempty,
    this.locationCommaOmitempty,
    this.startCommaOmitempty,
    this.endCommaOmitempty,
  });

  String id;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? summaryCommaOmitempty;

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

  String? startCommaOmitempty;

  String? endCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is UpdateEventRequest &&
    other.id == id &&
    other.summaryCommaOmitempty == summaryCommaOmitempty &&
    other.descriptionCommaOmitempty == descriptionCommaOmitempty &&
    other.locationCommaOmitempty == locationCommaOmitempty &&
    other.startCommaOmitempty == startCommaOmitempty &&
    other.endCommaOmitempty == endCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (summaryCommaOmitempty == null ? 0 : summaryCommaOmitempty!.hashCode) +
    (descriptionCommaOmitempty == null ? 0 : descriptionCommaOmitempty!.hashCode) +
    (locationCommaOmitempty == null ? 0 : locationCommaOmitempty!.hashCode) +
    (startCommaOmitempty == null ? 0 : startCommaOmitempty!.hashCode) +
    (endCommaOmitempty == null ? 0 : endCommaOmitempty!.hashCode);

  @override
  String toString() => 'UpdateEventRequest[id=$id, summaryCommaOmitempty=$summaryCommaOmitempty, descriptionCommaOmitempty=$descriptionCommaOmitempty, locationCommaOmitempty=$locationCommaOmitempty, startCommaOmitempty=$startCommaOmitempty, endCommaOmitempty=$endCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
    if (this.summaryCommaOmitempty != null) {
      json[r'summary,omitempty'] = this.summaryCommaOmitempty;
    } else {
      json[r'summary,omitempty'] = null;
    }
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
    if (this.startCommaOmitempty != null) {
      json[r'start,omitempty'] = this.startCommaOmitempty;
    } else {
      json[r'start,omitempty'] = null;
    }
    if (this.endCommaOmitempty != null) {
      json[r'end,omitempty'] = this.endCommaOmitempty;
    } else {
      json[r'end,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [UpdateEventRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static UpdateEventRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "UpdateEventRequest[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "UpdateEventRequest[id]" has a null value in JSON.');
        return true;
      }());

      return UpdateEventRequest(
        id: mapValueOfType<String>(json, r'id')!,
        summaryCommaOmitempty: mapValueOfType<String>(json, r'summary,omitempty'),
        descriptionCommaOmitempty: mapValueOfType<String>(json, r'description,omitempty'),
        locationCommaOmitempty: mapValueOfType<String>(json, r'location,omitempty'),
        startCommaOmitempty: mapValueOfType<String>(json, r'start,omitempty'),
        endCommaOmitempty: mapValueOfType<String>(json, r'end,omitempty'),
      );
    }
    return null;
  }

  static List<UpdateEventRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <UpdateEventRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = UpdateEventRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, UpdateEventRequest> mapFromJson(dynamic json) {
    final map = <String, UpdateEventRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = UpdateEventRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of UpdateEventRequest-objects as value to a dart map
  static Map<String, List<UpdateEventRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<UpdateEventRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = UpdateEventRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
  };
}

