//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ShardDetail {
  /// Returns a new [ShardDetail] instance.
  ShardDetail({
    required this.dimension,
    required this.m,
    required this.efConstruction,
    required this.efSearch,
    required this.vectorCount,
    required this.databaseSizeBytes,
    required this.shardId,
  });

  int dimension;

  int m;

  int efConstruction;

  int efSearch;

  int vectorCount;

  int databaseSizeBytes;

  String shardId;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ShardDetail &&
    other.dimension == dimension &&
    other.m == m &&
    other.efConstruction == efConstruction &&
    other.efSearch == efSearch &&
    other.vectorCount == vectorCount &&
    other.databaseSizeBytes == databaseSizeBytes &&
    other.shardId == shardId;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (dimension.hashCode) +
    (m.hashCode) +
    (efConstruction.hashCode) +
    (efSearch.hashCode) +
    (vectorCount.hashCode) +
    (databaseSizeBytes.hashCode) +
    (shardId.hashCode);

  @override
  String toString() => 'ShardDetail[dimension=$dimension, m=$m, efConstruction=$efConstruction, efSearch=$efSearch, vectorCount=$vectorCount, databaseSizeBytes=$databaseSizeBytes, shardId=$shardId]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'dimension'] = this.dimension;
      json[r'm'] = this.m;
      json[r'ef_construction'] = this.efConstruction;
      json[r'ef_search'] = this.efSearch;
      json[r'vector_count'] = this.vectorCount;
      json[r'database_size_bytes'] = this.databaseSizeBytes;
      json[r'shard_id'] = this.shardId;
    return json;
  }

  /// Returns a new [ShardDetail] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ShardDetail? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'dimension'), 'Required key "ShardDetail[dimension]" is missing from JSON.');
        assert(json[r'dimension'] != null, 'Required key "ShardDetail[dimension]" has a null value in JSON.');
        assert(json.containsKey(r'm'), 'Required key "ShardDetail[m]" is missing from JSON.');
        assert(json[r'm'] != null, 'Required key "ShardDetail[m]" has a null value in JSON.');
        assert(json.containsKey(r'ef_construction'), 'Required key "ShardDetail[ef_construction]" is missing from JSON.');
        assert(json[r'ef_construction'] != null, 'Required key "ShardDetail[ef_construction]" has a null value in JSON.');
        assert(json.containsKey(r'ef_search'), 'Required key "ShardDetail[ef_search]" is missing from JSON.');
        assert(json[r'ef_search'] != null, 'Required key "ShardDetail[ef_search]" has a null value in JSON.');
        assert(json.containsKey(r'vector_count'), 'Required key "ShardDetail[vector_count]" is missing from JSON.');
        assert(json[r'vector_count'] != null, 'Required key "ShardDetail[vector_count]" has a null value in JSON.');
        assert(json.containsKey(r'database_size_bytes'), 'Required key "ShardDetail[database_size_bytes]" is missing from JSON.');
        assert(json[r'database_size_bytes'] != null, 'Required key "ShardDetail[database_size_bytes]" has a null value in JSON.');
        assert(json.containsKey(r'shard_id'), 'Required key "ShardDetail[shard_id]" is missing from JSON.');
        assert(json[r'shard_id'] != null, 'Required key "ShardDetail[shard_id]" has a null value in JSON.');
        return true;
      }());

      return ShardDetail(
        dimension: mapValueOfType<int>(json, r'dimension')!,
        m: mapValueOfType<int>(json, r'm')!,
        efConstruction: mapValueOfType<int>(json, r'ef_construction')!,
        efSearch: mapValueOfType<int>(json, r'ef_search')!,
        vectorCount: mapValueOfType<int>(json, r'vector_count')!,
        databaseSizeBytes: mapValueOfType<int>(json, r'database_size_bytes')!,
        shardId: mapValueOfType<String>(json, r'shard_id')!,
      );
    }
    return null;
  }

  static List<ShardDetail> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ShardDetail>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ShardDetail.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ShardDetail> mapFromJson(dynamic json) {
    final map = <String, ShardDetail>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ShardDetail.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ShardDetail-objects as value to a dart map
  static Map<String, List<ShardDetail>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ShardDetail>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ShardDetail.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'dimension',
    'm',
    'ef_construction',
    'ef_search',
    'vector_count',
    'database_size_bytes',
    'shard_id',
  };
}

