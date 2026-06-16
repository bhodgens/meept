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

class ProviderInfo {
  /// Returns a new [ProviderInfo] instance.
  ProviderInfo({
    required this.id,
    required this.name,
    required this.api,
    required this.baseUrl,
    required this.models,
    required this.hasCredentials,
  });

  String id;

  String name;

  String api;

  String baseUrl;

  String? models;

  bool hasCredentials;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ProviderInfo &&
    other.id == id &&
    other.name == name &&
    other.api == api &&
    other.baseUrl == baseUrl &&
    other.models == models &&
    other.hasCredentials == hasCredentials;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (name.hashCode) +
    (api.hashCode) +
    (baseUrl.hashCode) +
    (models == null ? 0 : models!.hashCode) +
    (hasCredentials.hashCode);

  @override
  String toString() => 'ProviderInfo[id=$id, name=$name, api=$api, baseUrl=$baseUrl, models=$models, hasCredentials=$hasCredentials]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'name'] = this.name;
      json[r'api'] = this.api;
      json[r'base_url'] = this.baseUrl;
    if (this.models != null) {
      json[r'models'] = this.models;
    } else {
      json[r'models'] = null;
    }
      json[r'has_credentials'] = this.hasCredentials;
    return json;
  }

  /// Returns a new [ProviderInfo] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ProviderInfo? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "ProviderInfo[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "ProviderInfo[id]" has a null value in JSON.');
        assert(json.containsKey(r'name'), 'Required key "ProviderInfo[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "ProviderInfo[name]" has a null value in JSON.');
        assert(json.containsKey(r'api'), 'Required key "ProviderInfo[api]" is missing from JSON.');
        assert(json[r'api'] != null, 'Required key "ProviderInfo[api]" has a null value in JSON.');
        assert(json.containsKey(r'base_url'), 'Required key "ProviderInfo[base_url]" is missing from JSON.');
        assert(json[r'base_url'] != null, 'Required key "ProviderInfo[base_url]" has a null value in JSON.');
        assert(json.containsKey(r'models'), 'Required key "ProviderInfo[models]" is missing from JSON.');
        assert(json.containsKey(r'has_credentials'), 'Required key "ProviderInfo[has_credentials]" is missing from JSON.');
        assert(json[r'has_credentials'] != null, 'Required key "ProviderInfo[has_credentials]" has a null value in JSON.');
        return true;
      }());

      return ProviderInfo(
        id: mapValueOfType<String>(json, r'id')!,
        name: mapValueOfType<String>(json, r'name')!,
        api: mapValueOfType<String>(json, r'api')!,
        baseUrl: mapValueOfType<String>(json, r'base_url')!,
        models: mapValueOfType<String>(json, r'models'),
        hasCredentials: mapValueOfType<bool>(json, r'has_credentials')!,
      );
    }
    return null;
  }

  static List<ProviderInfo> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ProviderInfo>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ProviderInfo.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ProviderInfo> mapFromJson(dynamic json) {
    final map = <String, ProviderInfo>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ProviderInfo.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ProviderInfo-objects as value to a dart map
  static Map<String, List<ProviderInfo>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ProviderInfo>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ProviderInfo.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'name',
    'api',
    'base_url',
    'models',
    'has_credentials',
  };
}

