//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Pipeline {
  /// Returns a new [Pipeline] instance.
  Pipeline({
    required this.id,
    required this.name,
    required this.description,
    required this.status,
    this.steps = const [],
    required this.metadata,
    required this.createdAt,
    required this.updatedAt,
  });

  String id;

  String name;

  String description;

  String status;

  List<String>? steps;

  String? metadata;

  String createdAt;

  String updatedAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Pipeline &&
    other.id == id &&
    other.name == name &&
    other.description == description &&
    other.status == status &&
    _deepEquality.equals(other.steps, steps) &&
    other.metadata == metadata &&
    other.createdAt == createdAt &&
    other.updatedAt == updatedAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (name.hashCode) +
    (description.hashCode) +
    (status.hashCode) +
    (steps == null ? 0 : steps!.hashCode) +
    (metadata == null ? 0 : metadata!.hashCode) +
    (createdAt.hashCode) +
    (updatedAt.hashCode);

  @override
  String toString() => 'Pipeline[id=$id, name=$name, description=$description, status=$status, steps=$steps, metadata=$metadata, createdAt=$createdAt, updatedAt=$updatedAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'name'] = this.name;
      json[r'description'] = this.description;
      json[r'status'] = this.status;
    if (this.steps != null) {
      json[r'steps'] = this.steps;
    } else {
      json[r'steps'] = null;
    }
    if (this.metadata != null) {
      json[r'metadata'] = this.metadata;
    } else {
      json[r'metadata'] = null;
    }
      json[r'created_at'] = this.createdAt;
      json[r'updated_at'] = this.updatedAt;
    return json;
  }

  /// Returns a new [Pipeline] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Pipeline? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "Pipeline[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "Pipeline[id]" has a null value in JSON.');
        assert(json.containsKey(r'name'), 'Required key "Pipeline[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "Pipeline[name]" has a null value in JSON.');
        assert(json.containsKey(r'description'), 'Required key "Pipeline[description]" is missing from JSON.');
        assert(json[r'description'] != null, 'Required key "Pipeline[description]" has a null value in JSON.');
        assert(json.containsKey(r'status'), 'Required key "Pipeline[status]" is missing from JSON.');
        assert(json[r'status'] != null, 'Required key "Pipeline[status]" has a null value in JSON.');
        assert(json.containsKey(r'steps'), 'Required key "Pipeline[steps]" is missing from JSON.');
        assert(json.containsKey(r'metadata'), 'Required key "Pipeline[metadata]" is missing from JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "Pipeline[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "Pipeline[created_at]" has a null value in JSON.');
        assert(json.containsKey(r'updated_at'), 'Required key "Pipeline[updated_at]" is missing from JSON.');
        assert(json[r'updated_at'] != null, 'Required key "Pipeline[updated_at]" has a null value in JSON.');
        return true;
      }());

      return Pipeline(
        id: mapValueOfType<String>(json, r'id')!,
        name: mapValueOfType<String>(json, r'name')!,
        description: mapValueOfType<String>(json, r'description')!,
        status: mapValueOfType<String>(json, r'status')!,
        steps: json[r'steps'] is Iterable
            ? (json[r'steps'] as Iterable).cast<String>().toList(growable: false)
            : const [],
        metadata: mapValueOfType<String>(json, r'metadata'),
        createdAt: mapValueOfType<String>(json, r'created_at')!,
        updatedAt: mapValueOfType<String>(json, r'updated_at')!,
      );
    }
    return null;
  }

  static List<Pipeline> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Pipeline>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Pipeline.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Pipeline> mapFromJson(dynamic json) {
    final map = <String, Pipeline>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Pipeline.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Pipeline-objects as value to a dart map
  static Map<String, List<Pipeline>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Pipeline>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Pipeline.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'name',
    'description',
    'status',
    'steps',
    'metadata',
    'created_at',
    'updated_at',
  };
}

