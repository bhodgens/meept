//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class DeletePipelineRequest {
  /// Returns a new [DeletePipelineRequest] instance.
  DeletePipelineRequest({
    required this.pipelineId,
  });

  String pipelineId;

  @override
  bool operator ==(Object other) => identical(this, other) || other is DeletePipelineRequest &&
    other.pipelineId == pipelineId;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (pipelineId.hashCode);

  @override
  String toString() => 'DeletePipelineRequest[pipelineId=$pipelineId]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'pipeline_id'] = this.pipelineId;
    return json;
  }

  /// Returns a new [DeletePipelineRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static DeletePipelineRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'pipeline_id'), 'Required key "DeletePipelineRequest[pipeline_id]" is missing from JSON.');
        assert(json[r'pipeline_id'] != null, 'Required key "DeletePipelineRequest[pipeline_id]" has a null value in JSON.');
        return true;
      }());

      return DeletePipelineRequest(
        pipelineId: mapValueOfType<String>(json, r'pipeline_id')!,
      );
    }
    return null;
  }

  static List<DeletePipelineRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <DeletePipelineRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = DeletePipelineRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, DeletePipelineRequest> mapFromJson(dynamic json) {
    final map = <String, DeletePipelineRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = DeletePipelineRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of DeletePipelineRequest-objects as value to a dart map
  static Map<String, List<DeletePipelineRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<DeletePipelineRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = DeletePipelineRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'pipeline_id',
  };
}

