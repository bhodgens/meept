//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class CacheStatsResponse {
  /// Returns a new [CacheStatsResponse] instance.
  CacheStatsResponse({
    required this.hits,
    required this.misses,
    required this.size,
  });

  int hits;

  int misses;

  int size;

  @override
  bool operator ==(Object other) => identical(this, other) || other is CacheStatsResponse &&
    other.hits == hits &&
    other.misses == misses &&
    other.size == size;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (hits.hashCode) +
    (misses.hashCode) +
    (size.hashCode);

  @override
  String toString() => 'CacheStatsResponse[hits=$hits, misses=$misses, size=$size]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'hits'] = this.hits;
      json[r'misses'] = this.misses;
      json[r'size'] = this.size;
    return json;
  }

  /// Returns a new [CacheStatsResponse] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static CacheStatsResponse? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'hits'), 'Required key "CacheStatsResponse[hits]" is missing from JSON.');
        assert(json[r'hits'] != null, 'Required key "CacheStatsResponse[hits]" has a null value in JSON.');
        assert(json.containsKey(r'misses'), 'Required key "CacheStatsResponse[misses]" is missing from JSON.');
        assert(json[r'misses'] != null, 'Required key "CacheStatsResponse[misses]" has a null value in JSON.');
        assert(json.containsKey(r'size'), 'Required key "CacheStatsResponse[size]" is missing from JSON.');
        assert(json[r'size'] != null, 'Required key "CacheStatsResponse[size]" has a null value in JSON.');
        return true;
      }());

      return CacheStatsResponse(
        hits: mapValueOfType<int>(json, r'hits')!,
        misses: mapValueOfType<int>(json, r'misses')!,
        size: mapValueOfType<int>(json, r'size')!,
      );
    }
    return null;
  }

  static List<CacheStatsResponse> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <CacheStatsResponse>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = CacheStatsResponse.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, CacheStatsResponse> mapFromJson(dynamic json) {
    final map = <String, CacheStatsResponse>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = CacheStatsResponse.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of CacheStatsResponse-objects as value to a dart map
  static Map<String, List<CacheStatsResponse>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<CacheStatsResponse>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = CacheStatsResponse.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'hits',
    'misses',
    'size',
  };
}

