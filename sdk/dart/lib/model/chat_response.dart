//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

// Standalone model file
import 'dart:convert' show json;
import 'package:collection/collection.dart';

class ChatResponse {
  /// Returns a new [ChatResponse] instance.
  ChatResponse({
    required this.reply,
    this.modelCommaOmitempty,
    this.tokensUsedCommaOmitempty,
  });

  String reply;

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
  int? tokensUsedCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ChatResponse &&
    other.reply == reply &&
    other.modelCommaOmitempty == modelCommaOmitempty &&
    other.tokensUsedCommaOmitempty == tokensUsedCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (reply.hashCode) +
    (modelCommaOmitempty == null ? 0 : modelCommaOmitempty!.hashCode) +
    (tokensUsedCommaOmitempty == null ? 0 : tokensUsedCommaOmitempty!.hashCode);

  @override
  String toString() => 'ChatResponse[reply=$reply, modelCommaOmitempty=$modelCommaOmitempty, tokensUsedCommaOmitempty=$tokensUsedCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'reply'] = this.reply;
    if (this.modelCommaOmitempty != null) {
      json[r'model,omitempty'] = this.modelCommaOmitempty;
    } else {
      json[r'model,omitempty'] = null;
    }
    if (this.tokensUsedCommaOmitempty != null) {
      json[r'tokens_used,omitempty'] = this.tokensUsedCommaOmitempty;
    } else {
      json[r'tokens_used,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [ChatResponse] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ChatResponse? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'reply'), 'Required key "ChatResponse[reply]" is missing from JSON.');
        assert(json[r'reply'] != null, 'Required key "ChatResponse[reply]" has a null value in JSON.');
        return true;
      }());

      return ChatResponse(
        reply: mapValueOfType<String>(json, r'reply')!,
        modelCommaOmitempty: mapValueOfType<String>(json, r'model,omitempty'),
        tokensUsedCommaOmitempty: mapValueOfType<int>(json, r'tokens_used,omitempty'),
      );
    }
    return null;
  }

  static List<ChatResponse> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ChatResponse>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ChatResponse.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ChatResponse> mapFromJson(dynamic json) {
    final map = <String, ChatResponse>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ChatResponse.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ChatResponse-objects as value to a dart map
  static Map<String, List<ChatResponse>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ChatResponse>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ChatResponse.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'reply',
  };
}

