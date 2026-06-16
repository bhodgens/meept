//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class RuntimeService {
  /// Returns a new [RuntimeService] instance.
  RuntimeService({
    this.manager,
  });

  Object? manager;

  @override
  bool operator ==(Object other) => identical(this, other) || other is RuntimeService &&
    other.manager == manager;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (manager == null ? 0 : manager!.hashCode);

  @override
  String toString() => 'RuntimeService[manager=$manager]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.manager != null) {
      json[r'manager'] = this.manager;
    } else {
      json[r'manager'] = null;
    }
    return json;
  }

  /// Returns a new [RuntimeService] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static RuntimeService? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return RuntimeService(
        manager: mapValueOfType<Object>(json, r'manager'),
      );
    }
    return null;
  }

  static List<RuntimeService> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RuntimeService>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RuntimeService.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, RuntimeService> mapFromJson(dynamic json) {
    final map = <String, RuntimeService>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = RuntimeService.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of RuntimeService-objects as value to a dart map
  static Map<String, List<RuntimeService>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<RuntimeService>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = RuntimeService.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

