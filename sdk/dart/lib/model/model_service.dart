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

class ModelService {
  /// Returns a new [ModelService] instance.
  ModelService({
    this.configPath,
    this.credStore,
    this.stateDir,
  });

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? configPath;

  Object? credStore;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? stateDir;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ModelService &&
    other.configPath == configPath &&
    other.credStore == credStore &&
    other.stateDir == stateDir;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (configPath == null ? 0 : configPath!.hashCode) +
    (credStore == null ? 0 : credStore!.hashCode) +
    (stateDir == null ? 0 : stateDir!.hashCode);

  @override
  String toString() => 'ModelService[configPath=$configPath, credStore=$credStore, stateDir=$stateDir]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.configPath != null) {
      json[r'configPath'] = this.configPath;
    } else {
      json[r'configPath'] = null;
    }
    if (this.credStore != null) {
      json[r'credStore'] = this.credStore;
    } else {
      json[r'credStore'] = null;
    }
    if (this.stateDir != null) {
      json[r'stateDir'] = this.stateDir;
    } else {
      json[r'stateDir'] = null;
    }
    return json;
  }

  /// Returns a new [ModelService] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ModelService? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return ModelService(
        configPath: mapValueOfType<String>(json, r'configPath'),
        credStore: mapValueOfType<Object>(json, r'credStore'),
        stateDir: mapValueOfType<String>(json, r'stateDir'),
      );
    }
    return null;
  }

  static List<ModelService> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ModelService>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ModelService.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ModelService> mapFromJson(dynamic json) {
    final map = <String, ModelService>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ModelService.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ModelService-objects as value to a dart map
  static Map<String, List<ModelService>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ModelService>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ModelService.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

