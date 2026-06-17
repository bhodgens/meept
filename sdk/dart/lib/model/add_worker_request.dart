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

class AddWorkerRequest {
  /// Returns a new [AddWorkerRequest] instance.
  AddWorkerRequest({
    required this.id,
    required this.capabilities,
  });

  String id;

  String? capabilities;

  @override
  bool operator ==(Object other) => identical(this, other) || other is AddWorkerRequest &&
    other.id == id &&
    other.capabilities == capabilities;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (capabilities == null ? 0 : capabilities!.hashCode);

  @override
  String toString() => 'AddWorkerRequest[id=$id, capabilities=$capabilities]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
    if (this.capabilities != null) {
      json[r'capabilities'] = this.capabilities;
    } else {
      json[r'capabilities'] = null;
    }
    return json;
  }

  /// Returns a new [AddWorkerRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static AddWorkerRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "AddWorkerRequest[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "AddWorkerRequest[id]" has a null value in JSON.');
        assert(json.containsKey(r'capabilities'), 'Required key "AddWorkerRequest[capabilities]" is missing from JSON.');
        return true;
      }());

      return AddWorkerRequest(
        id: mapValueOfType<String>(json, r'id')!,
        capabilities: mapValueOfType<String>(json, r'capabilities'),
      );
    }
    return null;
  }

  static List<AddWorkerRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <AddWorkerRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = AddWorkerRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, AddWorkerRequest> mapFromJson(dynamic json) {
    final map = <String, AddWorkerRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = AddWorkerRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of AddWorkerRequest-objects as value to a dart map
  static Map<String, List<AddWorkerRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<AddWorkerRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = AddWorkerRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'capabilities',
  };
}

