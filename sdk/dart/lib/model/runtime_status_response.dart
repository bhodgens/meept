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

class RuntimeStatusResponse {
  /// Returns a new [RuntimeStatusResponse] instance.
  RuntimeStatusResponse({
    this.runtimes = const [],
  });

  List<String>? runtimes;

  @override
  bool operator ==(Object other) => identical(this, other) || other is RuntimeStatusResponse &&
    _deepEquality.equals(other.runtimes, runtimes);

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (runtimes == null ? 0 : runtimes!.hashCode);

  @override
  String toString() => 'RuntimeStatusResponse[runtimes=$runtimes]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.runtimes != null) {
      json[r'runtimes'] = this.runtimes;
    } else {
      json[r'runtimes'] = null;
    }
    return json;
  }

  /// Returns a new [RuntimeStatusResponse] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static RuntimeStatusResponse? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'runtimes'), 'Required key "RuntimeStatusResponse[runtimes]" is missing from JSON.');
        return true;
      }());

      return RuntimeStatusResponse(
        runtimes: json[r'runtimes'] is Iterable
            ? (json[r'runtimes'] as Iterable).cast<String>().toList(growable: false)
            : const [],
      );
    }
    return null;
  }

  static List<RuntimeStatusResponse> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RuntimeStatusResponse>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RuntimeStatusResponse.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, RuntimeStatusResponse> mapFromJson(dynamic json) {
    final map = <String, RuntimeStatusResponse>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = RuntimeStatusResponse.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of RuntimeStatusResponse-objects as value to a dart map
  static Map<String, List<RuntimeStatusResponse>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<RuntimeStatusResponse>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = RuntimeStatusResponse.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'runtimes',
  };
}

