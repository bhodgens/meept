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

class QueueStatusResponse {
  /// Returns a new [QueueStatusResponse] instance.
  QueueStatusResponse({
    required this.steeringDepth,
    required this.followupDepth,
    required this.isActive,
    required this.generation,
  });

  int steeringDepth;

  int followupDepth;

  bool isActive;

  int generation;

  @override
  bool operator ==(Object other) => identical(this, other) || other is QueueStatusResponse &&
    other.steeringDepth == steeringDepth &&
    other.followupDepth == followupDepth &&
    other.isActive == isActive &&
    other.generation == generation;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (steeringDepth.hashCode) +
    (followupDepth.hashCode) +
    (isActive.hashCode) +
    (generation.hashCode);

  @override
  String toString() => 'QueueStatusResponse[steeringDepth=$steeringDepth, followupDepth=$followupDepth, isActive=$isActive, generation=$generation]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'steering_depth'] = this.steeringDepth;
      json[r'followup_depth'] = this.followupDepth;
      json[r'is_active'] = this.isActive;
      json[r'generation'] = this.generation;
    return json;
  }

  /// Returns a new [QueueStatusResponse] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static QueueStatusResponse? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'steering_depth'), 'Required key "QueueStatusResponse[steering_depth]" is missing from JSON.');
        assert(json[r'steering_depth'] != null, 'Required key "QueueStatusResponse[steering_depth]" has a null value in JSON.');
        assert(json.containsKey(r'followup_depth'), 'Required key "QueueStatusResponse[followup_depth]" is missing from JSON.');
        assert(json[r'followup_depth'] != null, 'Required key "QueueStatusResponse[followup_depth]" has a null value in JSON.');
        assert(json.containsKey(r'is_active'), 'Required key "QueueStatusResponse[is_active]" is missing from JSON.');
        assert(json[r'is_active'] != null, 'Required key "QueueStatusResponse[is_active]" has a null value in JSON.');
        assert(json.containsKey(r'generation'), 'Required key "QueueStatusResponse[generation]" is missing from JSON.');
        assert(json[r'generation'] != null, 'Required key "QueueStatusResponse[generation]" has a null value in JSON.');
        return true;
      }());

      return QueueStatusResponse(
        steeringDepth: mapValueOfType<int>(json, r'steering_depth')!,
        followupDepth: mapValueOfType<int>(json, r'followup_depth')!,
        isActive: mapValueOfType<bool>(json, r'is_active')!,
        generation: mapValueOfType<int>(json, r'generation')!,
      );
    }
    return null;
  }

  static List<QueueStatusResponse> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <QueueStatusResponse>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = QueueStatusResponse.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, QueueStatusResponse> mapFromJson(dynamic json) {
    final map = <String, QueueStatusResponse>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = QueueStatusResponse.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of QueueStatusResponse-objects as value to a dart map
  static Map<String, List<QueueStatusResponse>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<QueueStatusResponse>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = QueueStatusResponse.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'steering_depth',
    'followup_depth',
    'is_active',
    'generation',
  };
}

