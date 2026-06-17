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

class SkillsService {
  /// Returns a new [SkillsService] instance.
  SkillsService({
    this.registry,
    this.executor,
  });

  Object? registry;

  Object? executor;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SkillsService &&
    other.registry == registry &&
    other.executor == executor;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (registry == null ? 0 : registry!.hashCode) +
    (executor == null ? 0 : executor!.hashCode);

  @override
  String toString() => 'SkillsService[registry=$registry, executor=$executor]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.registry != null) {
      json[r'registry'] = this.registry;
    } else {
      json[r'registry'] = null;
    }
    if (this.executor != null) {
      json[r'executor'] = this.executor;
    } else {
      json[r'executor'] = null;
    }
    return json;
  }

  /// Returns a new [SkillsService] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SkillsService? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return SkillsService(
        registry: mapValueOfType<Object>(json, r'registry'),
        executor: mapValueOfType<Object>(json, r'executor'),
      );
    }
    return null;
  }

  static List<SkillsService> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SkillsService>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SkillsService.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SkillsService> mapFromJson(dynamic json) {
    final map = <String, SkillsService>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SkillsService.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SkillsService-objects as value to a dart map
  static Map<String, List<SkillsService>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SkillsService>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SkillsService.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

