//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class PipelineStatusResponse {
  /// Returns a new [PipelineStatusResponse] instance.
  PipelineStatusResponse({
    required this.pipelineId,
    required this.name,
    required this.status,
    this.steps = const [],
    required this.createdAt,
    required this.updatedAt,
  });

  String pipelineId;

  String name;

  String status;

  List<String>? steps;

  String createdAt;

  String updatedAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is PipelineStatusResponse &&
    other.pipelineId == pipelineId &&
    other.name == name &&
    other.status == status &&
    _deepEquality.equals(other.steps, steps) &&
    other.createdAt == createdAt &&
    other.updatedAt == updatedAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (pipelineId.hashCode) +
    (name.hashCode) +
    (status.hashCode) +
    (steps == null ? 0 : steps!.hashCode) +
    (createdAt.hashCode) +
    (updatedAt.hashCode);

  @override
  String toString() => 'PipelineStatusResponse[pipelineId=$pipelineId, name=$name, status=$status, steps=$steps, createdAt=$createdAt, updatedAt=$updatedAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'pipeline_id'] = this.pipelineId;
      json[r'name'] = this.name;
      json[r'status'] = this.status;
    if (this.steps != null) {
      json[r'steps'] = this.steps;
    } else {
      json[r'steps'] = null;
    }
      json[r'created_at'] = this.createdAt;
      json[r'updated_at'] = this.updatedAt;
    return json;
  }

  /// Returns a new [PipelineStatusResponse] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static PipelineStatusResponse? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'pipeline_id'), 'Required key "PipelineStatusResponse[pipeline_id]" is missing from JSON.');
        assert(json[r'pipeline_id'] != null, 'Required key "PipelineStatusResponse[pipeline_id]" has a null value in JSON.');
        assert(json.containsKey(r'name'), 'Required key "PipelineStatusResponse[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "PipelineStatusResponse[name]" has a null value in JSON.');
        assert(json.containsKey(r'status'), 'Required key "PipelineStatusResponse[status]" is missing from JSON.');
        assert(json[r'status'] != null, 'Required key "PipelineStatusResponse[status]" has a null value in JSON.');
        assert(json.containsKey(r'steps'), 'Required key "PipelineStatusResponse[steps]" is missing from JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "PipelineStatusResponse[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "PipelineStatusResponse[created_at]" has a null value in JSON.');
        assert(json.containsKey(r'updated_at'), 'Required key "PipelineStatusResponse[updated_at]" is missing from JSON.');
        assert(json[r'updated_at'] != null, 'Required key "PipelineStatusResponse[updated_at]" has a null value in JSON.');
        return true;
      }());

      return PipelineStatusResponse(
        pipelineId: mapValueOfType<String>(json, r'pipeline_id')!,
        name: mapValueOfType<String>(json, r'name')!,
        status: mapValueOfType<String>(json, r'status')!,
        steps: json[r'steps'] is Iterable
            ? (json[r'steps'] as Iterable).cast<String>().toList(growable: false)
            : const [],
        createdAt: mapValueOfType<String>(json, r'created_at')!,
        updatedAt: mapValueOfType<String>(json, r'updated_at')!,
      );
    }
    return null;
  }

  static List<PipelineStatusResponse> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <PipelineStatusResponse>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = PipelineStatusResponse.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, PipelineStatusResponse> mapFromJson(dynamic json) {
    final map = <String, PipelineStatusResponse>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = PipelineStatusResponse.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of PipelineStatusResponse-objects as value to a dart map
  static Map<String, List<PipelineStatusResponse>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<PipelineStatusResponse>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = PipelineStatusResponse.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'pipeline_id',
    'name',
    'status',
    'steps',
    'created_at',
    'updated_at',
  };
}

