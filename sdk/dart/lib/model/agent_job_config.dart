//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class AgentJobConfig {
  /// Returns a new [AgentJobConfig] instance.
  AgentJobConfig({
    required this.prompt,
    this.contextCommaOmitempty,
    this.modelCommaOmitempty,
    this.maxTokensCommaOmitempty,
    this.temperatureCommaOmitempty,
  });

  String prompt;

  String? contextCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? modelCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? maxTokensCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  num? temperatureCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is AgentJobConfig &&
    other.prompt == prompt &&
    other.contextCommaOmitempty == contextCommaOmitempty &&
    other.modelCommaOmitempty == modelCommaOmitempty &&
    other.maxTokensCommaOmitempty == maxTokensCommaOmitempty &&
    other.temperatureCommaOmitempty == temperatureCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (prompt.hashCode) +
    (contextCommaOmitempty == null ? 0 : contextCommaOmitempty!.hashCode) +
    (modelCommaOmitempty == null ? 0 : modelCommaOmitempty!.hashCode) +
    (maxTokensCommaOmitempty == null ? 0 : maxTokensCommaOmitempty!.hashCode) +
    (temperatureCommaOmitempty == null ? 0 : temperatureCommaOmitempty!.hashCode);

  @override
  String toString() => 'AgentJobConfig[prompt=$prompt, contextCommaOmitempty=$contextCommaOmitempty, modelCommaOmitempty=$modelCommaOmitempty, maxTokensCommaOmitempty=$maxTokensCommaOmitempty, temperatureCommaOmitempty=$temperatureCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'prompt'] = this.prompt;
    if (this.contextCommaOmitempty != null) {
      json[r'context,omitempty'] = this.contextCommaOmitempty;
    } else {
      json[r'context,omitempty'] = null;
    }
    if (this.modelCommaOmitempty != null) {
      json[r'model,omitempty'] = this.modelCommaOmitempty;
    } else {
      json[r'model,omitempty'] = null;
    }
    if (this.maxTokensCommaOmitempty != null) {
      json[r'max_tokens,omitempty'] = this.maxTokensCommaOmitempty;
    } else {
      json[r'max_tokens,omitempty'] = null;
    }
    if (this.temperatureCommaOmitempty != null) {
      json[r'temperature,omitempty'] = this.temperatureCommaOmitempty;
    } else {
      json[r'temperature,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [AgentJobConfig] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static AgentJobConfig? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'prompt'), 'Required key "AgentJobConfig[prompt]" is missing from JSON.');
        assert(json[r'prompt'] != null, 'Required key "AgentJobConfig[prompt]" has a null value in JSON.');
        return true;
      }());

      return AgentJobConfig(
        prompt: mapValueOfType<String>(json, r'prompt')!,
        contextCommaOmitempty: mapValueOfType<String>(json, r'context,omitempty'),
        modelCommaOmitempty: mapValueOfType<String>(json, r'model,omitempty'),
        maxTokensCommaOmitempty: mapValueOfType<int>(json, r'max_tokens,omitempty'),
        temperatureCommaOmitempty: num.parse('${json[r'temperature,omitempty']}'),
      );
    }
    return null;
  }

  static List<AgentJobConfig> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <AgentJobConfig>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = AgentJobConfig.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, AgentJobConfig> mapFromJson(dynamic json) {
    final map = <String, AgentJobConfig>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = AgentJobConfig.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of AgentJobConfig-objects as value to a dart map
  static Map<String, List<AgentJobConfig>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<AgentJobConfig>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = AgentJobConfig.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'prompt',
  };
}

