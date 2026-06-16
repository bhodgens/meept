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

class BusService {
  /// Returns a new [BusService] instance.
  BusService({
    this.bus,
  });

  Object? bus;

  @override
  bool operator ==(Object other) => identical(this, other) || other is BusService &&
    other.bus == bus;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (bus == null ? 0 : bus!.hashCode);

  @override
  String toString() => 'BusService[bus=$bus]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.bus != null) {
      json[r'bus'] = this.bus;
    } else {
      json[r'bus'] = null;
    }
    return json;
  }

  /// Returns a new [BusService] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static BusService? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return BusService(
        bus: mapValueOfType<Object>(json, r'bus'),
      );
    }
    return null;
  }

  static List<BusService> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <BusService>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = BusService.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, BusService> mapFromJson(dynamic json) {
    final map = <String, BusService>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = BusService.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of BusService-objects as value to a dart map
  static Map<String, List<BusService>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<BusService>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = BusService.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

