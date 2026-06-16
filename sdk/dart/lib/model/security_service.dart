//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class SecurityService {
  /// Returns a new [SecurityService] instance.
  SecurityService({
    this.checker,
  });

  Object? checker;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SecurityService &&
    other.checker == checker;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (checker == null ? 0 : checker!.hashCode);

  @override
  String toString() => 'SecurityService[checker=$checker]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.checker != null) {
      json[r'checker'] = this.checker;
    } else {
      json[r'checker'] = null;
    }
    return json;
  }

  /// Returns a new [SecurityService] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SecurityService? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return SecurityService(
        checker: mapValueOfType<Object>(json, r'checker'),
      );
    }
    return null;
  }

  static List<SecurityService> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SecurityService>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SecurityService.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SecurityService> mapFromJson(dynamic json) {
    final map = <String, SecurityService>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SecurityService.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SecurityService-objects as value to a dart map
  static Map<String, List<SecurityService>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SecurityService>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SecurityService.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

