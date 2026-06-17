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

class AuditEntry {
  /// Returns a new [AuditEntry] instance.
  AuditEntry({
    required this.timestamp,
    required this.action,
    required this.resource,
    required this.allowed,
  });

  String timestamp;

  String action;

  String resource;

  bool allowed;

  @override
  bool operator ==(Object other) => identical(this, other) || other is AuditEntry &&
    other.timestamp == timestamp &&
    other.action == action &&
    other.resource == resource &&
    other.allowed == allowed;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (timestamp.hashCode) +
    (action.hashCode) +
    (resource.hashCode) +
    (allowed.hashCode);

  @override
  String toString() => 'AuditEntry[timestamp=$timestamp, action=$action, resource=$resource, allowed=$allowed]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'timestamp'] = this.timestamp;
      json[r'action'] = this.action;
      json[r'resource'] = this.resource;
      json[r'allowed'] = this.allowed;
    return json;
  }

  /// Returns a new [AuditEntry] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static AuditEntry? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'timestamp'), 'Required key "AuditEntry[timestamp]" is missing from JSON.');
        assert(json[r'timestamp'] != null, 'Required key "AuditEntry[timestamp]" has a null value in JSON.');
        assert(json.containsKey(r'action'), 'Required key "AuditEntry[action]" is missing from JSON.');
        assert(json[r'action'] != null, 'Required key "AuditEntry[action]" has a null value in JSON.');
        assert(json.containsKey(r'resource'), 'Required key "AuditEntry[resource]" is missing from JSON.');
        assert(json[r'resource'] != null, 'Required key "AuditEntry[resource]" has a null value in JSON.');
        assert(json.containsKey(r'allowed'), 'Required key "AuditEntry[allowed]" is missing from JSON.');
        assert(json[r'allowed'] != null, 'Required key "AuditEntry[allowed]" has a null value in JSON.');
        return true;
      }());

      return AuditEntry(
        timestamp: mapValueOfType<String>(json, r'timestamp')!,
        action: mapValueOfType<String>(json, r'action')!,
        resource: mapValueOfType<String>(json, r'resource')!,
        allowed: mapValueOfType<bool>(json, r'allowed')!,
      );
    }
    return null;
  }

  static List<AuditEntry> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <AuditEntry>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = AuditEntry.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, AuditEntry> mapFromJson(dynamic json) {
    final map = <String, AuditEntry>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = AuditEntry.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of AuditEntry-objects as value to a dart map
  static Map<String, List<AuditEntry>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<AuditEntry>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = AuditEntry.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'timestamp',
    'action',
    'resource',
    'allowed',
  };
}

