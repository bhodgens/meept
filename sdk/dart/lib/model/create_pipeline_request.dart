//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class CreatePipelineRequest {
  /// Returns a new [CreatePipelineRequest] instance.
  CreatePipelineRequest({
    this.idCommaOmitempty,
    required this.name,
    this.descriptionCommaOmitempty,
    this.stepsCommaOmitempty = const [],
    this.metadataCommaOmitempty,
  });

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? idCommaOmitempty;

  String name;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? descriptionCommaOmitempty;

  List<String>? stepsCommaOmitempty;

  String? metadataCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is CreatePipelineRequest &&
    other.idCommaOmitempty == idCommaOmitempty &&
    other.name == name &&
    other.descriptionCommaOmitempty == descriptionCommaOmitempty &&
    _deepEquality.equals(other.stepsCommaOmitempty, stepsCommaOmitempty) &&
    other.metadataCommaOmitempty == metadataCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (idCommaOmitempty == null ? 0 : idCommaOmitempty!.hashCode) +
    (name.hashCode) +
    (descriptionCommaOmitempty == null ? 0 : descriptionCommaOmitempty!.hashCode) +
    (stepsCommaOmitempty == null ? 0 : stepsCommaOmitempty!.hashCode) +
    (metadataCommaOmitempty == null ? 0 : metadataCommaOmitempty!.hashCode);

  @override
  String toString() => 'CreatePipelineRequest[idCommaOmitempty=$idCommaOmitempty, name=$name, descriptionCommaOmitempty=$descriptionCommaOmitempty, stepsCommaOmitempty=$stepsCommaOmitempty, metadataCommaOmitempty=$metadataCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.idCommaOmitempty != null) {
      json[r'id,omitempty'] = this.idCommaOmitempty;
    } else {
      json[r'id,omitempty'] = null;
    }
      json[r'name'] = this.name;
    if (this.descriptionCommaOmitempty != null) {
      json[r'description,omitempty'] = this.descriptionCommaOmitempty;
    } else {
      json[r'description,omitempty'] = null;
    }
    if (this.stepsCommaOmitempty != null) {
      json[r'steps,omitempty'] = this.stepsCommaOmitempty;
    } else {
      json[r'steps,omitempty'] = null;
    }
    if (this.metadataCommaOmitempty != null) {
      json[r'metadata,omitempty'] = this.metadataCommaOmitempty;
    } else {
      json[r'metadata,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [CreatePipelineRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static CreatePipelineRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'name'), 'Required key "CreatePipelineRequest[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "CreatePipelineRequest[name]" has a null value in JSON.');
        return true;
      }());

      return CreatePipelineRequest(
        idCommaOmitempty: mapValueOfType<String>(json, r'id,omitempty'),
        name: mapValueOfType<String>(json, r'name')!,
        descriptionCommaOmitempty: mapValueOfType<String>(json, r'description,omitempty'),
        stepsCommaOmitempty: json[r'steps,omitempty'] is Iterable
            ? (json[r'steps,omitempty'] as Iterable).cast<String>().toList(growable: false)
            : const [],
        metadataCommaOmitempty: mapValueOfType<String>(json, r'metadata,omitempty'),
      );
    }
    return null;
  }

  static List<CreatePipelineRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <CreatePipelineRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = CreatePipelineRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, CreatePipelineRequest> mapFromJson(dynamic json) {
    final map = <String, CreatePipelineRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = CreatePipelineRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of CreatePipelineRequest-objects as value to a dart map
  static Map<String, List<CreatePipelineRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<CreatePipelineRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = CreatePipelineRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'name',
  };
}

