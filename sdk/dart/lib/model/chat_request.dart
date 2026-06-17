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

class ChatRequest {
  /// Returns a new [ChatRequest] instance.
  ChatRequest({
    required this.message,
    required this.conversationId,
    this.agentIdCommaOmitempty,
  });

  String message;

  String conversationId;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? agentIdCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ChatRequest &&
    other.message == message &&
    other.conversationId == conversationId &&
    other.agentIdCommaOmitempty == agentIdCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (message.hashCode) +
    (conversationId.hashCode) +
    (agentIdCommaOmitempty == null ? 0 : agentIdCommaOmitempty!.hashCode);

  @override
  String toString() => 'ChatRequest[message=$message, conversationId=$conversationId, agentIdCommaOmitempty=$agentIdCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'message'] = this.message;
      json[r'conversation_id'] = this.conversationId;
    if (this.agentIdCommaOmitempty != null) {
      json[r'agent_id,omitempty'] = this.agentIdCommaOmitempty;
    } else {
      json[r'agent_id,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [ChatRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ChatRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'message'), 'Required key "ChatRequest[message]" is missing from JSON.');
        assert(json[r'message'] != null, 'Required key "ChatRequest[message]" has a null value in JSON.');
        assert(json.containsKey(r'conversation_id'), 'Required key "ChatRequest[conversation_id]" is missing from JSON.');
        assert(json[r'conversation_id'] != null, 'Required key "ChatRequest[conversation_id]" has a null value in JSON.');
        return true;
      }());

      return ChatRequest(
        message: mapValueOfType<String>(json, r'message')!,
        conversationId: mapValueOfType<String>(json, r'conversation_id')!,
        agentIdCommaOmitempty: mapValueOfType<String>(json, r'agent_id,omitempty'),
      );
    }
    return null;
  }

  static List<ChatRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ChatRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ChatRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ChatRequest> mapFromJson(dynamic json) {
    final map = <String, ChatRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ChatRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ChatRequest-objects as value to a dart map
  static Map<String, List<ChatRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ChatRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ChatRequest.listFromJson(entry.value, growable: growable,);
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

