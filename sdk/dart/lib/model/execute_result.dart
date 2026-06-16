//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ExecuteResult {
  /// Returns a new [ExecuteResult] instance.
  ExecuteResult({
    required this.output,
    required this.success,
    this.errorCommaOmitempty,
  });

  String output;

  bool success;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? errorCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ExecuteResult &&
    other.output == output &&
    other.success == success &&
    other.errorCommaOmitempty == errorCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (output.hashCode) +
    (success.hashCode) +
    (errorCommaOmitempty == null ? 0 : errorCommaOmitempty!.hashCode);

  @override
  String toString() => 'ExecuteResult[output=$output, success=$success, errorCommaOmitempty=$errorCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'output'] = this.output;
      json[r'success'] = this.success;
    if (this.errorCommaOmitempty != null) {
      json[r'error,omitempty'] = this.errorCommaOmitempty;
    } else {
      json[r'error,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [ExecuteResult] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ExecuteResult? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'output'), 'Required key "ExecuteResult[output]" is missing from JSON.');
        assert(json[r'output'] != null, 'Required key "ExecuteResult[output]" has a null value in JSON.');
        assert(json.containsKey(r'success'), 'Required key "ExecuteResult[success]" is missing from JSON.');
        assert(json[r'success'] != null, 'Required key "ExecuteResult[success]" has a null value in JSON.');
        return true;
      }());

      return ExecuteResult(
        output: mapValueOfType<String>(json, r'output')!,
        success: mapValueOfType<bool>(json, r'success')!,
        errorCommaOmitempty: mapValueOfType<String>(json, r'error,omitempty'),
      );
    }
    return null;
  }

  static List<ExecuteResult> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ExecuteResult>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ExecuteResult.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ExecuteResult> mapFromJson(dynamic json) {
    final map = <String, ExecuteResult>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ExecuteResult.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ExecuteResult-objects as value to a dart map
  static Map<String, List<ExecuteResult>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ExecuteResult>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ExecuteResult.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'output',
    'success',
  };
}

