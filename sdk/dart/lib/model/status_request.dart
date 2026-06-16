//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class StatusRequest {
  /// Returns a new [StatusRequest] instance.
  StatusRequest({
    required this.pipelineId,
  });

  String pipelineId;

  @override
  bool operator ==(Object other) => identical(this, other) || other is StatusRequest &&
    other.pipelineId == pipelineId;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (pipelineId.hashCode);

  @override
  String toString() => 'StatusRequest[pipelineId=$pipelineId]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'pipeline_id'] = this.pipelineId;
    return json;
  }

  /// Returns a new [StatusRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static StatusRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'pipeline_id'), 'Required key "StatusRequest[pipeline_id]" is missing from JSON.');
        assert(json[r'pipeline_id'] != null, 'Required key "StatusRequest[pipeline_id]" has a null value in JSON.');
        return true;
      }());

      return StatusRequest(
        pipelineId: mapValueOfType<String>(json, r'pipeline_id')!,
      );
    }
    return null;
  }

  static List<StatusRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <StatusRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = StatusRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, StatusRequest> mapFromJson(dynamic json) {
    final map = <String, StatusRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = StatusRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of StatusRequest-objects as value to a dart map
  static Map<String, List<StatusRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<StatusRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = StatusRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'pipeline_id',
  };
}

