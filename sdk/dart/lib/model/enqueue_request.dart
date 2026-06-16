//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class EnqueueRequest {
  /// Returns a new [EnqueueRequest] instance.
  EnqueueRequest({
    required this.type,
    this.priorityCommaOmitempty,
    this.taskIdCommaOmitempty,
    required this.prompt,
    this.sessionIdCommaOmitempty,
    this.requiredCapsCommaOmitempty,
    this.payloadCommaOmitempty,
  });

  String type;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? priorityCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? taskIdCommaOmitempty;

  String prompt;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? sessionIdCommaOmitempty;

  String? requiredCapsCommaOmitempty;

  String? payloadCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is EnqueueRequest &&
    other.type == type &&
    other.priorityCommaOmitempty == priorityCommaOmitempty &&
    other.taskIdCommaOmitempty == taskIdCommaOmitempty &&
    other.prompt == prompt &&
    other.sessionIdCommaOmitempty == sessionIdCommaOmitempty &&
    other.requiredCapsCommaOmitempty == requiredCapsCommaOmitempty &&
    other.payloadCommaOmitempty == payloadCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (type.hashCode) +
    (priorityCommaOmitempty == null ? 0 : priorityCommaOmitempty!.hashCode) +
    (taskIdCommaOmitempty == null ? 0 : taskIdCommaOmitempty!.hashCode) +
    (prompt.hashCode) +
    (sessionIdCommaOmitempty == null ? 0 : sessionIdCommaOmitempty!.hashCode) +
    (requiredCapsCommaOmitempty == null ? 0 : requiredCapsCommaOmitempty!.hashCode) +
    (payloadCommaOmitempty == null ? 0 : payloadCommaOmitempty!.hashCode);

  @override
  String toString() => 'EnqueueRequest[type=$type, priorityCommaOmitempty=$priorityCommaOmitempty, taskIdCommaOmitempty=$taskIdCommaOmitempty, prompt=$prompt, sessionIdCommaOmitempty=$sessionIdCommaOmitempty, requiredCapsCommaOmitempty=$requiredCapsCommaOmitempty, payloadCommaOmitempty=$payloadCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'type'] = this.type;
    if (this.priorityCommaOmitempty != null) {
      json[r'priority,omitempty'] = this.priorityCommaOmitempty;
    } else {
      json[r'priority,omitempty'] = null;
    }
    if (this.taskIdCommaOmitempty != null) {
      json[r'task_id,omitempty'] = this.taskIdCommaOmitempty;
    } else {
      json[r'task_id,omitempty'] = null;
    }
      json[r'prompt'] = this.prompt;
    if (this.sessionIdCommaOmitempty != null) {
      json[r'session_id,omitempty'] = this.sessionIdCommaOmitempty;
    } else {
      json[r'session_id,omitempty'] = null;
    }
    if (this.requiredCapsCommaOmitempty != null) {
      json[r'required_caps,omitempty'] = this.requiredCapsCommaOmitempty;
    } else {
      json[r'required_caps,omitempty'] = null;
    }
    if (this.payloadCommaOmitempty != null) {
      json[r'payload,omitempty'] = this.payloadCommaOmitempty;
    } else {
      json[r'payload,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [EnqueueRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static EnqueueRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'type'), 'Required key "EnqueueRequest[type]" is missing from JSON.');
        assert(json[r'type'] != null, 'Required key "EnqueueRequest[type]" has a null value in JSON.');
        assert(json.containsKey(r'prompt'), 'Required key "EnqueueRequest[prompt]" is missing from JSON.');
        assert(json[r'prompt'] != null, 'Required key "EnqueueRequest[prompt]" has a null value in JSON.');
        return true;
      }());

      return EnqueueRequest(
        type: mapValueOfType<String>(json, r'type')!,
        priorityCommaOmitempty: mapValueOfType<int>(json, r'priority,omitempty'),
        taskIdCommaOmitempty: mapValueOfType<String>(json, r'task_id,omitempty'),
        prompt: mapValueOfType<String>(json, r'prompt')!,
        sessionIdCommaOmitempty: mapValueOfType<String>(json, r'session_id,omitempty'),
        requiredCapsCommaOmitempty: mapValueOfType<String>(json, r'required_caps,omitempty'),
        payloadCommaOmitempty: mapValueOfType<String>(json, r'payload,omitempty'),
      );
    }
    return null;
  }

  static List<EnqueueRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <EnqueueRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = EnqueueRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, EnqueueRequest> mapFromJson(dynamic json) {
    final map = <String, EnqueueRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = EnqueueRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of EnqueueRequest-objects as value to a dart map
  static Map<String, List<EnqueueRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<EnqueueRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = EnqueueRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'type',
    'prompt',
  };
}

