//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class PublishRequest {
  /// Returns a new [PublishRequest] instance.
  PublishRequest({
    required this.topic,
    required this.type,
    this.sourceCommaOmitempty,
    this.payloadCommaOmitempty,
  });

  String topic;

  String type;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? sourceCommaOmitempty;

  String? payloadCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is PublishRequest &&
    other.topic == topic &&
    other.type == type &&
    other.sourceCommaOmitempty == sourceCommaOmitempty &&
    other.payloadCommaOmitempty == payloadCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (topic.hashCode) +
    (type.hashCode) +
    (sourceCommaOmitempty == null ? 0 : sourceCommaOmitempty!.hashCode) +
    (payloadCommaOmitempty == null ? 0 : payloadCommaOmitempty!.hashCode);

  @override
  String toString() => 'PublishRequest[topic=$topic, type=$type, sourceCommaOmitempty=$sourceCommaOmitempty, payloadCommaOmitempty=$payloadCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'topic'] = this.topic;
      json[r'type'] = this.type;
    if (this.sourceCommaOmitempty != null) {
      json[r'source,omitempty'] = this.sourceCommaOmitempty;
    } else {
      json[r'source,omitempty'] = null;
    }
    if (this.payloadCommaOmitempty != null) {
      json[r'payload,omitempty'] = this.payloadCommaOmitempty;
    } else {
      json[r'payload,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [PublishRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static PublishRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'topic'), 'Required key "PublishRequest[topic]" is missing from JSON.');
        assert(json[r'topic'] != null, 'Required key "PublishRequest[topic]" has a null value in JSON.');
        assert(json.containsKey(r'type'), 'Required key "PublishRequest[type]" is missing from JSON.');
        assert(json[r'type'] != null, 'Required key "PublishRequest[type]" has a null value in JSON.');
        return true;
      }());

      return PublishRequest(
        topic: mapValueOfType<String>(json, r'topic')!,
        type: mapValueOfType<String>(json, r'type')!,
        sourceCommaOmitempty: mapValueOfType<String>(json, r'source,omitempty'),
        payloadCommaOmitempty: mapValueOfType<String>(json, r'payload,omitempty'),
      );
    }
    return null;
  }

  static List<PublishRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <PublishRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = PublishRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, PublishRequest> mapFromJson(dynamic json) {
    final map = <String, PublishRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = PublishRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of PublishRequest-objects as value to a dart map
  static Map<String, List<PublishRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<PublishRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = PublishRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'topic',
    'type',
  };
}

