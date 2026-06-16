//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class SteerRequest {
  /// Returns a new [SteerRequest] instance.
  SteerRequest({
    required this.message,
    required this.conversationId,
    this.sourceCommaOmitempty,
  });

  String message;

  String conversationId;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? sourceCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SteerRequest &&
    other.message == message &&
    other.conversationId == conversationId &&
    other.sourceCommaOmitempty == sourceCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (message.hashCode) +
    (conversationId.hashCode) +
    (sourceCommaOmitempty == null ? 0 : sourceCommaOmitempty!.hashCode);

  @override
  String toString() => 'SteerRequest[message=$message, conversationId=$conversationId, sourceCommaOmitempty=$sourceCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'message'] = this.message;
      json[r'conversation_id'] = this.conversationId;
    if (this.sourceCommaOmitempty != null) {
      json[r'source,omitempty'] = this.sourceCommaOmitempty;
    } else {
      json[r'source,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [SteerRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SteerRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'message'), 'Required key "SteerRequest[message]" is missing from JSON.');
        assert(json[r'message'] != null, 'Required key "SteerRequest[message]" has a null value in JSON.');
        assert(json.containsKey(r'conversation_id'), 'Required key "SteerRequest[conversation_id]" is missing from JSON.');
        assert(json[r'conversation_id'] != null, 'Required key "SteerRequest[conversation_id]" has a null value in JSON.');
        return true;
      }());

      return SteerRequest(
        message: mapValueOfType<String>(json, r'message')!,
        conversationId: mapValueOfType<String>(json, r'conversation_id')!,
        sourceCommaOmitempty: mapValueOfType<String>(json, r'source,omitempty'),
      );
    }
    return null;
  }

  static List<SteerRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SteerRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SteerRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SteerRequest> mapFromJson(dynamic json) {
    final map = <String, SteerRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SteerRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SteerRequest-objects as value to a dart map
  static Map<String, List<SteerRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SteerRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SteerRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'message',
    'conversation_id',
  };
}

