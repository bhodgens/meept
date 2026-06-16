//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class VectorStats {
  /// Returns a new [VectorStats] instance.
  VectorStats({
    required this.loadedShards,
    required this.maxRamShards,
    required this.lruHits,
    required this.lruMisses,
    required this.lruEvictions,
    required this.shardDetails,
  });

  int loadedShards;

  int maxRamShards;

  int lruHits;

  int lruMisses;

  int lruEvictions;

  String? shardDetails;

  @override
  bool operator ==(Object other) => identical(this, other) || other is VectorStats &&
    other.loadedShards == loadedShards &&
    other.maxRamShards == maxRamShards &&
    other.lruHits == lruHits &&
    other.lruMisses == lruMisses &&
    other.lruEvictions == lruEvictions &&
    other.shardDetails == shardDetails;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (loadedShards.hashCode) +
    (maxRamShards.hashCode) +
    (lruHits.hashCode) +
    (lruMisses.hashCode) +
    (lruEvictions.hashCode) +
    (shardDetails == null ? 0 : shardDetails!.hashCode);

  @override
  String toString() => 'VectorStats[loadedShards=$loadedShards, maxRamShards=$maxRamShards, lruHits=$lruHits, lruMisses=$lruMisses, lruEvictions=$lruEvictions, shardDetails=$shardDetails]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'loaded_shards'] = this.loadedShards;
      json[r'max_ram_shards'] = this.maxRamShards;
      json[r'lru_hits'] = this.lruHits;
      json[r'lru_misses'] = this.lruMisses;
      json[r'lru_evictions'] = this.lruEvictions;
    if (this.shardDetails != null) {
      json[r'shard_details'] = this.shardDetails;
    } else {
      json[r'shard_details'] = null;
    }
    return json;
  }

  /// Returns a new [VectorStats] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static VectorStats? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'loaded_shards'), 'Required key "VectorStats[loaded_shards]" is missing from JSON.');
        assert(json[r'loaded_shards'] != null, 'Required key "VectorStats[loaded_shards]" has a null value in JSON.');
        assert(json.containsKey(r'max_ram_shards'), 'Required key "VectorStats[max_ram_shards]" is missing from JSON.');
        assert(json[r'max_ram_shards'] != null, 'Required key "VectorStats[max_ram_shards]" has a null value in JSON.');
        assert(json.containsKey(r'lru_hits'), 'Required key "VectorStats[lru_hits]" is missing from JSON.');
        assert(json[r'lru_hits'] != null, 'Required key "VectorStats[lru_hits]" has a null value in JSON.');
        assert(json.containsKey(r'lru_misses'), 'Required key "VectorStats[lru_misses]" is missing from JSON.');
        assert(json[r'lru_misses'] != null, 'Required key "VectorStats[lru_misses]" has a null value in JSON.');
        assert(json.containsKey(r'lru_evictions'), 'Required key "VectorStats[lru_evictions]" is missing from JSON.');
        assert(json[r'lru_evictions'] != null, 'Required key "VectorStats[lru_evictions]" has a null value in JSON.');
        assert(json.containsKey(r'shard_details'), 'Required key "VectorStats[shard_details]" is missing from JSON.');
        return true;
      }());

      return VectorStats(
        loadedShards: mapValueOfType<int>(json, r'loaded_shards')!,
        maxRamShards: mapValueOfType<int>(json, r'max_ram_shards')!,
        lruHits: mapValueOfType<int>(json, r'lru_hits')!,
        lruMisses: mapValueOfType<int>(json, r'lru_misses')!,
        lruEvictions: mapValueOfType<int>(json, r'lru_evictions')!,
        shardDetails: mapValueOfType<String>(json, r'shard_details'),
      );
    }
    return null;
  }

  static List<VectorStats> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <VectorStats>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = VectorStats.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, VectorStats> mapFromJson(dynamic json) {
    final map = <String, VectorStats>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = VectorStats.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of VectorStats-objects as value to a dart map
  static Map<String, List<VectorStats>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<VectorStats>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = VectorStats.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'loaded_shards',
    'max_ram_shards',
    'lru_hits',
    'lru_misses',
    'lru_evictions',
    'shard_details',
  };
}

