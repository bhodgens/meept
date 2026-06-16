//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class TemplatesInvokeRequest {
  /// Returns a new [TemplatesInvokeRequest] instance.
  TemplatesInvokeRequest({
    required this.name,
    this.argsCommaOmitempty,
  });

  String name;

  String? argsCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is TemplatesInvokeRequest &&
    other.name == name &&
    other.argsCommaOmitempty == argsCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (name.hashCode) +
    (argsCommaOmitempty == null ? 0 : argsCommaOmitempty!.hashCode);

  @override
  String toString() => 'TemplatesInvokeRequest[name=$name, argsCommaOmitempty=$argsCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'name'] = this.name;
    if (this.argsCommaOmitempty != null) {
      json[r'args,omitempty'] = this.argsCommaOmitempty;
    } else {
      json[r'args,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [TemplatesInvokeRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static TemplatesInvokeRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'name'), 'Required key "TemplatesInvokeRequest[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "TemplatesInvokeRequest[name]" has a null value in JSON.');
        return true;
      }());

      return TemplatesInvokeRequest(
        name: mapValueOfType<String>(json, r'name')!,
        argsCommaOmitempty: mapValueOfType<String>(json, r'args,omitempty'),
      );
    }
    return null;
  }

  static List<TemplatesInvokeRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TemplatesInvokeRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TemplatesInvokeRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, TemplatesInvokeRequest> mapFromJson(dynamic json) {
    final map = <String, TemplatesInvokeRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = TemplatesInvokeRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of TemplatesInvokeRequest-objects as value to a dart map
  static Map<String, List<TemplatesInvokeRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<TemplatesInvokeRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = TemplatesInvokeRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'name',
  };
}

