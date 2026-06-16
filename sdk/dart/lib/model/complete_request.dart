//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class CompleteRequest {
  /// Returns a new [CompleteRequest] instance.
  CompleteRequest({
    required this.jobId,
    this.resultCommaOmitempty,
  });

  String jobId;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? resultCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is CompleteRequest &&
    other.jobId == jobId &&
    other.resultCommaOmitempty == resultCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (jobId.hashCode) +
    (resultCommaOmitempty == null ? 0 : resultCommaOmitempty!.hashCode);

  @override
  String toString() => 'CompleteRequest[jobId=$jobId, resultCommaOmitempty=$resultCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'job_id'] = this.jobId;
    if (this.resultCommaOmitempty != null) {
      json[r'result,omitempty'] = this.resultCommaOmitempty;
    } else {
      json[r'result,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [CompleteRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static CompleteRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'job_id'), 'Required key "CompleteRequest[job_id]" is missing from JSON.');
        assert(json[r'job_id'] != null, 'Required key "CompleteRequest[job_id]" has a null value in JSON.');
        return true;
      }());

      return CompleteRequest(
        jobId: mapValueOfType<String>(json, r'job_id')!,
        resultCommaOmitempty: mapValueOfType<Object>(json, r'result,omitempty'),
      );
    }
    return null;
  }

  static List<CompleteRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <CompleteRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = CompleteRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, CompleteRequest> mapFromJson(dynamic json) {
    final map = <String, CompleteRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = CompleteRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of CompleteRequest-objects as value to a dart map
  static Map<String, List<CompleteRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<CompleteRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = CompleteRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'job_id',
  };
}

