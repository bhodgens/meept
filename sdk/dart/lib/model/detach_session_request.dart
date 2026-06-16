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

class DetachSessionRequest {
  /// Returns a new [DetachSessionRequest] instance.
  DetachSessionRequest({
    required this.id,
    required this.agentId,
  });

  String id;

  String agentId;

  @override
  bool operator ==(Object other) => identical(this, other) || other is DetachSessionRequest &&
    other.id == id &&
    other.agentId == agentId;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (agentId.hashCode);

  @override
  String toString() => 'DetachSessionRequest[id=$id, agentId=$agentId]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'agent_id'] = this.agentId;
    return json;
  }

  /// Returns a new [DetachSessionRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static DetachSessionRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "DetachSessionRequest[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "DetachSessionRequest[id]" has a null value in JSON.');
        assert(json.containsKey(r'agent_id'), 'Required key "DetachSessionRequest[agent_id]" is missing from JSON.');
        assert(json[r'agent_id'] != null, 'Required key "DetachSessionRequest[agent_id]" has a null value in JSON.');
        return true;
      }());

      return DetachSessionRequest(
        id: mapValueOfType<String>(json, r'id')!,
        agentId: mapValueOfType<String>(json, r'agent_id')!,
      );
    }
    return null;
  }

  static List<DetachSessionRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <DetachSessionRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = DetachSessionRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, DetachSessionRequest> mapFromJson(dynamic json) {
    final map = <String, DetachSessionRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = DetachSessionRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of DetachSessionRequest-objects as value to a dart map
  static Map<String, List<DetachSessionRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<DetachSessionRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = DetachSessionRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'agent_id',
  };
}

