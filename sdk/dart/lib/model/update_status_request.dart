//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class UpdateStatusRequest {
  /// Returns a new [UpdateStatusRequest] instance.
  UpdateStatusRequest({
    required this.pipelineId,
    required this.status,
  });

  String pipelineId;

  String status;

  @override
  bool operator ==(Object other) => identical(this, other) || other is UpdateStatusRequest &&
    other.pipelineId == pipelineId &&
    other.status == status;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (pipelineId.hashCode) +
    (status.hashCode);

  @override
  String toString() => 'UpdateStatusRequest[pipelineId=$pipelineId, status=$status]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'pipeline_id'] = this.pipelineId;
      json[r'status'] = this.status;
    return json;
  }

  /// Returns a new [UpdateStatusRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static UpdateStatusRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'pipeline_id'), 'Required key "UpdateStatusRequest[pipeline_id]" is missing from JSON.');
        assert(json[r'pipeline_id'] != null, 'Required key "UpdateStatusRequest[pipeline_id]" has a null value in JSON.');
        assert(json.containsKey(r'status'), 'Required key "UpdateStatusRequest[status]" is missing from JSON.');
        assert(json[r'status'] != null, 'Required key "UpdateStatusRequest[status]" has a null value in JSON.');
        return true;
      }());

      return UpdateStatusRequest(
        pipelineId: mapValueOfType<String>(json, r'pipeline_id')!,
        status: mapValueOfType<String>(json, r'status')!,
      );
    }
    return null;
  }

  static List<UpdateStatusRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <UpdateStatusRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = UpdateStatusRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, UpdateStatusRequest> mapFromJson(dynamic json) {
    final map = <String, UpdateStatusRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = UpdateStatusRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of UpdateStatusRequest-objects as value to a dart map
  static Map<String, List<UpdateStatusRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<UpdateStatusRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = UpdateStatusRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'pipeline_id',
    'status',
  };
}

