//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ModelInfo {
  /// Returns a new [ModelInfo] instance.
  ModelInfo({
    required this.provider,
    required this.model,
    required this.fullName,
    required this.baseUrl,
    required this.contextLimit,
    required this.maxOutput,
    required this.capabilities,
    required this.isDefault,
    required this.inputCost,
    required this.outputCost,
  });

  String provider;

  String model;

  String fullName;

  String baseUrl;

  int contextLimit;

  int maxOutput;

  String? capabilities;

  bool isDefault;

  num inputCost;

  num outputCost;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ModelInfo &&
    other.provider == provider &&
    other.model == model &&
    other.fullName == fullName &&
    other.baseUrl == baseUrl &&
    other.contextLimit == contextLimit &&
    other.maxOutput == maxOutput &&
    other.capabilities == capabilities &&
    other.isDefault == isDefault &&
    other.inputCost == inputCost &&
    other.outputCost == outputCost;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (provider.hashCode) +
    (model.hashCode) +
    (fullName.hashCode) +
    (baseUrl.hashCode) +
    (contextLimit.hashCode) +
    (maxOutput.hashCode) +
    (capabilities == null ? 0 : capabilities!.hashCode) +
    (isDefault.hashCode) +
    (inputCost.hashCode) +
    (outputCost.hashCode);

  @override
  String toString() => 'ModelInfo[provider=$provider, model=$model, fullName=$fullName, baseUrl=$baseUrl, contextLimit=$contextLimit, maxOutput=$maxOutput, capabilities=$capabilities, isDefault=$isDefault, inputCost=$inputCost, outputCost=$outputCost]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'provider'] = this.provider;
      json[r'model'] = this.model;
      json[r'full_name'] = this.fullName;
      json[r'base_url'] = this.baseUrl;
      json[r'context_limit'] = this.contextLimit;
      json[r'max_output'] = this.maxOutput;
    if (this.capabilities != null) {
      json[r'capabilities'] = this.capabilities;
    } else {
      json[r'capabilities'] = null;
    }
      json[r'is_default'] = this.isDefault;
      json[r'input_cost'] = this.inputCost;
      json[r'output_cost'] = this.outputCost;
    return json;
  }

  /// Returns a new [ModelInfo] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ModelInfo? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'provider'), 'Required key "ModelInfo[provider]" is missing from JSON.');
        assert(json[r'provider'] != null, 'Required key "ModelInfo[provider]" has a null value in JSON.');
        assert(json.containsKey(r'model'), 'Required key "ModelInfo[model]" is missing from JSON.');
        assert(json[r'model'] != null, 'Required key "ModelInfo[model]" has a null value in JSON.');
        assert(json.containsKey(r'full_name'), 'Required key "ModelInfo[full_name]" is missing from JSON.');
        assert(json[r'full_name'] != null, 'Required key "ModelInfo[full_name]" has a null value in JSON.');
        assert(json.containsKey(r'base_url'), 'Required key "ModelInfo[base_url]" is missing from JSON.');
        assert(json[r'base_url'] != null, 'Required key "ModelInfo[base_url]" has a null value in JSON.');
        assert(json.containsKey(r'context_limit'), 'Required key "ModelInfo[context_limit]" is missing from JSON.');
        assert(json[r'context_limit'] != null, 'Required key "ModelInfo[context_limit]" has a null value in JSON.');
        assert(json.containsKey(r'max_output'), 'Required key "ModelInfo[max_output]" is missing from JSON.');
        assert(json[r'max_output'] != null, 'Required key "ModelInfo[max_output]" has a null value in JSON.');
        assert(json.containsKey(r'capabilities'), 'Required key "ModelInfo[capabilities]" is missing from JSON.');
        assert(json.containsKey(r'is_default'), 'Required key "ModelInfo[is_default]" is missing from JSON.');
        assert(json[r'is_default'] != null, 'Required key "ModelInfo[is_default]" has a null value in JSON.');
        assert(json.containsKey(r'input_cost'), 'Required key "ModelInfo[input_cost]" is missing from JSON.');
        assert(json[r'input_cost'] != null, 'Required key "ModelInfo[input_cost]" has a null value in JSON.');
        assert(json.containsKey(r'output_cost'), 'Required key "ModelInfo[output_cost]" is missing from JSON.');
        assert(json[r'output_cost'] != null, 'Required key "ModelInfo[output_cost]" has a null value in JSON.');
        return true;
      }());

      return ModelInfo(
        provider: mapValueOfType<String>(json, r'provider')!,
        model: mapValueOfType<String>(json, r'model')!,
        fullName: mapValueOfType<String>(json, r'full_name')!,
        baseUrl: mapValueOfType<String>(json, r'base_url')!,
        contextLimit: mapValueOfType<int>(json, r'context_limit')!,
        maxOutput: mapValueOfType<int>(json, r'max_output')!,
        capabilities: mapValueOfType<String>(json, r'capabilities'),
        isDefault: mapValueOfType<bool>(json, r'is_default')!,
        inputCost: num.parse('${json[r'input_cost']}'),
        outputCost: num.parse('${json[r'output_cost']}'),
      );
    }
    return null;
  }

  static List<ModelInfo> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ModelInfo>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ModelInfo.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ModelInfo> mapFromJson(dynamic json) {
    final map = <String, ModelInfo>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ModelInfo.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ModelInfo-objects as value to a dart map
  static Map<String, List<ModelInfo>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ModelInfo>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ModelInfo.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'provider',
    'model',
    'full_name',
    'base_url',
    'context_limit',
    'max_output',
    'capabilities',
    'is_default',
    'input_cost',
    'output_cost',
  };
}

