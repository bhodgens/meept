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

class DaemonService {
  /// Returns a new [DaemonService] instance.
  DaemonService({
    this.pidFile,
    this.stateDir,
    this.binPath,
    this.controller,
  });

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? pidFile;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? stateDir;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? binPath;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? controller;

  @override
  bool operator ==(Object other) => identical(this, other) || other is DaemonService &&
    other.pidFile == pidFile &&
    other.stateDir == stateDir &&
    other.binPath == binPath &&
    other.controller == controller;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (pidFile == null ? 0 : pidFile!.hashCode) +
    (stateDir == null ? 0 : stateDir!.hashCode) +
    (binPath == null ? 0 : binPath!.hashCode) +
    (controller == null ? 0 : controller!.hashCode);

  @override
  String toString() => 'DaemonService[pidFile=$pidFile, stateDir=$stateDir, binPath=$binPath, controller=$controller]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.pidFile != null) {
      json[r'pidFile'] = this.pidFile;
    } else {
      json[r'pidFile'] = null;
    }
    if (this.stateDir != null) {
      json[r'stateDir'] = this.stateDir;
    } else {
      json[r'stateDir'] = null;
    }
    if (this.binPath != null) {
      json[r'binPath'] = this.binPath;
    } else {
      json[r'binPath'] = null;
    }
    if (this.controller != null) {
      json[r'controller'] = this.controller;
    } else {
      json[r'controller'] = null;
    }
    return json;
  }

  /// Returns a new [DaemonService] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static DaemonService? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return DaemonService(
        pidFile: mapValueOfType<String>(json, r'pidFile'),
        stateDir: mapValueOfType<String>(json, r'stateDir'),
        binPath: mapValueOfType<String>(json, r'binPath'),
        controller: mapValueOfType<Object>(json, r'controller'),
      );
    }
    return null;
  }

  static List<DaemonService> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <DaemonService>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = DaemonService.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, DaemonService> mapFromJson(dynamic json) {
    final map = <String, DaemonService>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = DaemonService.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of DaemonService-objects as value to a dart map
  static Map<String, List<DaemonService>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<DaemonService>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = DaemonService.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

