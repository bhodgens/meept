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

class RejectPlanRequest {
  /// Returns a new [RejectPlanRequest] instance.
  RejectPlanRequest({
    required this.planId,
    required this.sessionId,
    required this.by,
    this.reasonCommaOmitempty,
  });

  String planId;

  String sessionId;

  String by;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? reasonCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is RejectPlanRequest &&
    other.planId == planId &&
    other.sessionId == sessionId &&
    other.by == by &&
    other.reasonCommaOmitempty == reasonCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (planId.hashCode) +
    (sessionId.hashCode) +
    (by.hashCode) +
    (reasonCommaOmitempty == null ? 0 : reasonCommaOmitempty!.hashCode);

  @override
  String toString() => 'RejectPlanRequest[planId=$planId, sessionId=$sessionId, by=$by, reasonCommaOmitempty=$reasonCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'plan_id'] = this.planId;
      json[r'session_id'] = this.sessionId;
      json[r'by'] = this.by;
    if (this.reasonCommaOmitempty != null) {
      json[r'reason,omitempty'] = this.reasonCommaOmitempty;
    } else {
      json[r'reason,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [RejectPlanRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static RejectPlanRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'plan_id'), 'Required key "RejectPlanRequest[plan_id]" is missing from JSON.');
        assert(json[r'plan_id'] != null, 'Required key "RejectPlanRequest[plan_id]" has a null value in JSON.');
        assert(json.containsKey(r'session_id'), 'Required key "RejectPlanRequest[session_id]" is missing from JSON.');
        assert(json[r'session_id'] != null, 'Required key "RejectPlanRequest[session_id]" has a null value in JSON.');
        assert(json.containsKey(r'by'), 'Required key "RejectPlanRequest[by]" is missing from JSON.');
        assert(json[r'by'] != null, 'Required key "RejectPlanRequest[by]" has a null value in JSON.');
        return true;
      }());

      return RejectPlanRequest(
        planId: mapValueOfType<String>(json, r'plan_id')!,
        sessionId: mapValueOfType<String>(json, r'session_id')!,
        by: mapValueOfType<String>(json, r'by')!,
        reasonCommaOmitempty: mapValueOfType<String>(json, r'reason,omitempty'),
      );
    }
    return null;
  }

  static List<RejectPlanRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RejectPlanRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RejectPlanRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, RejectPlanRequest> mapFromJson(dynamic json) {
    final map = <String, RejectPlanRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = RejectPlanRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of RejectPlanRequest-objects as value to a dart map
  static Map<String, List<RejectPlanRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<RejectPlanRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = RejectPlanRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'plan_id',
    'session_id',
    'by',
  };
}

