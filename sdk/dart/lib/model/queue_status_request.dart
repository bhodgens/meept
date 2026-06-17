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

class QueueStatusRequest {
  /// Returns a new [QueueStatusRequest] instance.
  QueueStatusRequest({
    required this.conversationId,
  });

  String conversationId;

  @override
  bool operator ==(Object other) => identical(this, other) || other is QueueStatusRequest &&
    other.conversationId == conversationId;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (conversationId.hashCode);

  @override
  String toString() => 'QueueStatusRequest[conversationId=$conversationId]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'conversation_id'] = this.conversationId;
    return json;
  }

  /// Returns a new [QueueStatusRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static QueueStatusRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'conversation_id'), 'Required key "QueueStatusRequest[conversation_id]" is missing from JSON.');
        assert(json[r'conversation_id'] != null, 'Required key "QueueStatusRequest[conversation_id]" has a null value in JSON.');
        return true;
      }());

      return QueueStatusRequest(
        conversationId: mapValueOfType<String>(json, r'conversation_id')!,
      );
    }
    return null;
  }

  static List<QueueStatusRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <QueueStatusRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = QueueStatusRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, QueueStatusRequest> mapFromJson(dynamic json) {
    final map = <String, QueueStatusRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = QueueStatusRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of QueueStatusRequest-objects as value to a dart map
  static Map<String, List<QueueStatusRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<QueueStatusRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = QueueStatusRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'conversation_id',
  };
}

