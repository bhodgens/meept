//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class TemplatesInvokeResult {
  /// Returns a new [TemplatesInvokeResult] instance.
  TemplatesInvokeResult({
    required this.prompt,
    this.outputCommaOmitempty,
    required this.success,
    this.errorCommaOmitempty,
  });

  String prompt;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? outputCommaOmitempty;

  bool success;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? errorCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is TemplatesInvokeResult &&
    other.prompt == prompt &&
    other.outputCommaOmitempty == outputCommaOmitempty &&
    other.success == success &&
    other.errorCommaOmitempty == errorCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (prompt.hashCode) +
    (outputCommaOmitempty == null ? 0 : outputCommaOmitempty!.hashCode) +
    (success.hashCode) +
    (errorCommaOmitempty == null ? 0 : errorCommaOmitempty!.hashCode);

  @override
  String toString() => 'TemplatesInvokeResult[prompt=$prompt, outputCommaOmitempty=$outputCommaOmitempty, success=$success, errorCommaOmitempty=$errorCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'prompt'] = this.prompt;
    if (this.outputCommaOmitempty != null) {
      json[r'output,omitempty'] = this.outputCommaOmitempty;
    } else {
      json[r'output,omitempty'] = null;
    }
      json[r'success'] = this.success;
    if (this.errorCommaOmitempty != null) {
      json[r'error,omitempty'] = this.errorCommaOmitempty;
    } else {
      json[r'error,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [TemplatesInvokeResult] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static TemplatesInvokeResult? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'prompt'), 'Required key "TemplatesInvokeResult[prompt]" is missing from JSON.');
        assert(json[r'prompt'] != null, 'Required key "TemplatesInvokeResult[prompt]" has a null value in JSON.');
        assert(json.containsKey(r'success'), 'Required key "TemplatesInvokeResult[success]" is missing from JSON.');
        assert(json[r'success'] != null, 'Required key "TemplatesInvokeResult[success]" has a null value in JSON.');
        return true;
      }());

      return TemplatesInvokeResult(
        prompt: mapValueOfType<String>(json, r'prompt')!,
        outputCommaOmitempty: mapValueOfType<String>(json, r'output,omitempty'),
        success: mapValueOfType<bool>(json, r'success')!,
        errorCommaOmitempty: mapValueOfType<String>(json, r'error,omitempty'),
      );
    }
    return null;
  }

  static List<TemplatesInvokeResult> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TemplatesInvokeResult>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TemplatesInvokeResult.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, TemplatesInvokeResult> mapFromJson(dynamic json) {
    final map = <String, TemplatesInvokeResult>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = TemplatesInvokeResult.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of TemplatesInvokeResult-objects as value to a dart map
  static Map<String, List<TemplatesInvokeResult>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<TemplatesInvokeResult>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = TemplatesInvokeResult.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'prompt',
    'success',
  };
}

