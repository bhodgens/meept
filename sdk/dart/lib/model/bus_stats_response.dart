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

class BusStatsResponse {
  /// Returns a new [BusStatsResponse] instance.
  BusStatsResponse({
    required this.subscribers,
    required this.messagesSent,
    required this.queuedMessages,
  });

  int subscribers;

  int messagesSent;

  int queuedMessages;

  @override
  bool operator ==(Object other) => identical(this, other) || other is BusStatsResponse &&
    other.subscribers == subscribers &&
    other.messagesSent == messagesSent &&
    other.queuedMessages == queuedMessages;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (subscribers.hashCode) +
    (messagesSent.hashCode) +
    (queuedMessages.hashCode);

  @override
  String toString() => 'BusStatsResponse[subscribers=$subscribers, messagesSent=$messagesSent, queuedMessages=$queuedMessages]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'subscribers'] = this.subscribers;
      json[r'messages_sent'] = this.messagesSent;
      json[r'queued_messages'] = this.queuedMessages;
    return json;
  }

  /// Returns a new [BusStatsResponse] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static BusStatsResponse? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'subscribers'), 'Required key "BusStatsResponse[subscribers]" is missing from JSON.');
        assert(json[r'subscribers'] != null, 'Required key "BusStatsResponse[subscribers]" has a null value in JSON.');
        assert(json.containsKey(r'messages_sent'), 'Required key "BusStatsResponse[messages_sent]" is missing from JSON.');
        assert(json[r'messages_sent'] != null, 'Required key "BusStatsResponse[messages_sent]" has a null value in JSON.');
        assert(json.containsKey(r'queued_messages'), 'Required key "BusStatsResponse[queued_messages]" is missing from JSON.');
        assert(json[r'queued_messages'] != null, 'Required key "BusStatsResponse[queued_messages]" has a null value in JSON.');
        return true;
      }());

      return BusStatsResponse(
        subscribers: mapValueOfType<int>(json, r'subscribers')!,
        messagesSent: mapValueOfType<int>(json, r'messages_sent')!,
        queuedMessages: mapValueOfType<int>(json, r'queued_messages')!,
      );
    }
    return null;
  }

  static List<BusStatsResponse> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <BusStatsResponse>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = BusStatsResponse.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, BusStatsResponse> mapFromJson(dynamic json) {
    final map = <String, BusStatsResponse>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = BusStatsResponse.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of BusStatsResponse-objects as value to a dart map
  static Map<String, List<BusStatsResponse>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<BusStatsResponse>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = BusStatsResponse.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'subscribers',
    'messages_sent',
    'queued_messages',
  };
}

