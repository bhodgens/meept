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

class ServiceError {
  /// Returns a new [ServiceError] instance.
  ServiceError({
    this.service,
    this.op,
    this.err,
  });

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? service;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? op;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? err;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ServiceError &&
    other.service == service &&
    other.op == op &&
    other.err == err;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (service == null ? 0 : service!.hashCode) +
    (op == null ? 0 : op!.hashCode) +
    (err == null ? 0 : err!.hashCode);

  @override
  String toString() => 'ServiceError[service=$service, op=$op, err=$err]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.service != null) {
      json[r'Service'] = this.service;
    } else {
      json[r'Service'] = null;
    }
    if (this.op != null) {
      json[r'Op'] = this.op;
    } else {
      json[r'Op'] = null;
    }
    if (this.err != null) {
      json[r'Err'] = this.err;
    } else {
      json[r'Err'] = null;
    }
    return json;
  }

  /// Returns a new [ServiceError] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ServiceError? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return ServiceError(
        service: mapValueOfType<String>(json, r'Service'),
        op: mapValueOfType<String>(json, r'Op'),
        err: mapValueOfType<Object>(json, r'Err'),
      );
    }
    return null;
  }

  static List<ServiceError> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ServiceError>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ServiceError.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ServiceError> mapFromJson(dynamic json) {
    final map = <String, ServiceError>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ServiceError.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ServiceError-objects as value to a dart map
  static Map<String, List<ServiceError>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ServiceError>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ServiceError.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

