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

class AgentProgressEvent {
  /// Returns a new [AgentProgressEvent] instance.
  AgentProgressEvent({
    this.type,
    this.sessionId,
    this.agentId,
    this.message,
    this.tier,
    this.sourceEvent,
    this.timestamp,
  });

  AgentProgressEventTypeEnum? type;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? sessionId;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? agentId;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? message;

  /// Minimum value: 0
  /// Maximum value: 2
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? tier;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? sourceEvent;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? timestamp;

  @override
  bool operator ==(Object other) => identical(this, other) || other is AgentProgressEvent &&
    other.type == type &&
    other.sessionId == sessionId &&
    other.agentId == agentId &&
    other.message == message &&
    other.tier == tier &&
    other.sourceEvent == sourceEvent &&
    other.timestamp == timestamp;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (type == null ? 0 : type!.hashCode) +
    (sessionId == null ? 0 : sessionId!.hashCode) +
    (agentId == null ? 0 : agentId!.hashCode) +
    (message == null ? 0 : message!.hashCode) +
    (tier == null ? 0 : tier!.hashCode) +
    (sourceEvent == null ? 0 : sourceEvent!.hashCode) +
    (timestamp == null ? 0 : timestamp!.hashCode);

  @override
  String toString() => 'AgentProgressEvent[type=$type, sessionId=$sessionId, agentId=$agentId, message=$message, tier=$tier, sourceEvent=$sourceEvent, timestamp=$timestamp]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.type != null) {
      json[r'type'] = this.type;
    } else {
      json[r'type'] = null;
    }
    if (this.sessionId != null) {
      json[r'session_id'] = this.sessionId;
    } else {
      json[r'session_id'] = null;
    }
    if (this.agentId != null) {
      json[r'agent_id'] = this.agentId;
    } else {
      json[r'agent_id'] = null;
    }
    if (this.message != null) {
      json[r'message'] = this.message;
    } else {
      json[r'message'] = null;
    }
    if (this.tier != null) {
      json[r'tier'] = this.tier;
    } else {
      json[r'tier'] = null;
    }
    if (this.sourceEvent != null) {
      json[r'source_event'] = this.sourceEvent;
    } else {
      json[r'source_event'] = null;
    }
    if (this.timestamp != null) {
      json[r'timestamp'] = this.timestamp!.toUtc().toIso8601String();
    } else {
      json[r'timestamp'] = null;
    }
    return json;
  }

  /// Returns a new [AgentProgressEvent] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static AgentProgressEvent? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return AgentProgressEvent(
        type: AgentProgressEventTypeEnum.fromJson(json[r'type']),
        sessionId: mapValueOfType<String>(json, r'session_id'),
        agentId: mapValueOfType<String>(json, r'agent_id'),
        message: mapValueOfType<String>(json, r'message'),
        tier: mapValueOfType<int>(json, r'tier'),
        sourceEvent: mapValueOfType<String>(json, r'source_event'),
        timestamp: mapDateTime(json, r'timestamp', r''),
      );
    }
    return null;
  }

  static List<AgentProgressEvent> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <AgentProgressEvent>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = AgentProgressEvent.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, AgentProgressEvent> mapFromJson(dynamic json) {
    final map = <String, AgentProgressEvent>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = AgentProgressEvent.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of AgentProgressEvent-objects as value to a dart map
  static Map<String, List<AgentProgressEvent>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<AgentProgressEvent>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = AgentProgressEvent.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}


class AgentProgressEventTypeEnum {
  /// Instantiate a new enum with the provided [value].
  const AgentProgressEventTypeEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const agentProgress = AgentProgressEventTypeEnum._(r'agent_progress');

  /// List of all possible values in this [enum][AgentProgressEventTypeEnum].
  static const values = <AgentProgressEventTypeEnum>[
    agentProgress,
  ];

  static AgentProgressEventTypeEnum? fromJson(dynamic value) => AgentProgressEventTypeEnumTypeTransformer().decode(value);

  static List<AgentProgressEventTypeEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <AgentProgressEventTypeEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = AgentProgressEventTypeEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [AgentProgressEventTypeEnum] to String,
/// and [decode] dynamic data back to [AgentProgressEventTypeEnum].
class AgentProgressEventTypeEnumTypeTransformer {
  factory AgentProgressEventTypeEnumTypeTransformer() => _instance ??= const AgentProgressEventTypeEnumTypeTransformer._();

  const AgentProgressEventTypeEnumTypeTransformer._();

  String encode(AgentProgressEventTypeEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a AgentProgressEventTypeEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  AgentProgressEventTypeEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'agent_progress': return AgentProgressEventTypeEnum.agentProgress;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [AgentProgressEventTypeEnumTypeTransformer] instance.
  static AgentProgressEventTypeEnumTypeTransformer? _instance;
}


