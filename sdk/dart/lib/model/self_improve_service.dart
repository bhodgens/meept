//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class SelfImproveService {
  /// Returns a new [SelfImproveService] instance.
  SelfImproveService({
    this.controller,
  });

  Object? controller;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SelfImproveService &&
    other.controller == controller;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (controller == null ? 0 : controller!.hashCode);

  @override
  String toString() => 'SelfImproveService[controller=$controller]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.controller != null) {
      json[r'controller'] = this.controller;
    } else {
      json[r'controller'] = null;
    }
    return json;
  }

  /// Returns a new [SelfImproveService] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SelfImproveService? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return SelfImproveService(
        controller: mapValueOfType<Object>(json, r'controller'),
      );
    }
    return null;
  }

  static List<SelfImproveService> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SelfImproveService>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SelfImproveService.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SelfImproveService> mapFromJson(dynamic json) {
    final map = <String, SelfImproveService>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SelfImproveService.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SelfImproveService-objects as value to a dart map
  static Map<String, List<SelfImproveService>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SelfImproveService>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SelfImproveService.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

