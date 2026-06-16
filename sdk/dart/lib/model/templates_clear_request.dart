//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class TemplatesClearRequest {
  /// Returns a new [TemplatesClearRequest] instance.
  TemplatesClearRequest({
    required this.conversationId,
    this.nameCommaOmitempty,
  });

  String conversationId;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? nameCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is TemplatesClearRequest &&
    other.conversationId == conversationId &&
    other.nameCommaOmitempty == nameCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (conversationId.hashCode) +
    (nameCommaOmitempty == null ? 0 : nameCommaOmitempty!.hashCode);

  @override
  String toString() => 'TemplatesClearRequest[conversationId=$conversationId, nameCommaOmitempty=$nameCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'conversation_id'] = this.conversationId;
    if (this.nameCommaOmitempty != null) {
      json[r'name,omitempty'] = this.nameCommaOmitempty;
    } else {
      json[r'name,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [TemplatesClearRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static TemplatesClearRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'conversation_id'), 'Required key "TemplatesClearRequest[conversation_id]" is missing from JSON.');
        assert(json[r'conversation_id'] != null, 'Required key "TemplatesClearRequest[conversation_id]" has a null value in JSON.');
        return true;
      }());

      return TemplatesClearRequest(
        conversationId: mapValueOfType<String>(json, r'conversation_id')!,
        nameCommaOmitempty: mapValueOfType<String>(json, r'name,omitempty'),
      );
    }
    return null;
  }

  static List<TemplatesClearRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TemplatesClearRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TemplatesClearRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, TemplatesClearRequest> mapFromJson(dynamic json) {
    final map = <String, TemplatesClearRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = TemplatesClearRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of TemplatesClearRequest-objects as value to a dart map
  static Map<String, List<TemplatesClearRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<TemplatesClearRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = TemplatesClearRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'conversation_id',
  };
}

