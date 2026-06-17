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

class PipelineService {
  /// Returns a new [PipelineService] instance.
  PipelineService({
    this.mu,
    this.pipelines,
  });

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? mu;

  String? pipelines;

  @override
  bool operator ==(Object other) => identical(this, other) || other is PipelineService &&
    other.mu == mu &&
    other.pipelines == pipelines;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (mu == null ? 0 : mu!.hashCode) +
    (pipelines == null ? 0 : pipelines!.hashCode);

  @override
  String toString() => 'PipelineService[mu=$mu, pipelines=$pipelines]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.mu != null) {
      json[r'mu'] = this.mu;
    } else {
      json[r'mu'] = null;
    }
    if (this.pipelines != null) {
      json[r'pipelines'] = this.pipelines;
    } else {
      json[r'pipelines'] = null;
    }
    return json;
  }

  /// Returns a new [PipelineService] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static PipelineService? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return PipelineService(
        mu: mapValueOfType<Object>(json, r'mu'),
        pipelines: mapValueOfType<String>(json, r'pipelines'),
      );
    }
    return null;
  }

  static List<PipelineService> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <PipelineService>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = PipelineService.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, PipelineService> mapFromJson(dynamic json) {
    final map = <String, PipelineService>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = PipelineService.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of PipelineService-objects as value to a dart map
  static Map<String, List<PipelineService>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<PipelineService>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = PipelineService.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

