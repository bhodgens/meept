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

class WSSubscribeMessage {
  /// Returns a new [WSSubscribeMessage] instance.
  WSSubscribeMessage({
    this.type,
    this.channel,
    this.sessionId,
  });

  WSSubscribeMessageTypeEnum? type;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? channel;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? sessionId;

  @override
  bool operator ==(Object other) => identical(this, other) || other is WSSubscribeMessage &&
    other.type == type &&
    other.channel == channel &&
    other.sessionId == sessionId;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (type == null ? 0 : type!.hashCode) +
    (channel == null ? 0 : channel!.hashCode) +
    (sessionId == null ? 0 : sessionId!.hashCode);

  @override
  String toString() => 'WSSubscribeMessage[type=$type, channel=$channel, sessionId=$sessionId]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.type != null) {
      json[r'type'] = this.type;
    } else {
      json[r'type'] = null;
    }
    if (this.channel != null) {
      json[r'channel'] = this.channel;
    } else {
      json[r'channel'] = null;
    }
    if (this.sessionId != null) {
      json[r'session_id'] = this.sessionId;
    } else {
      json[r'session_id'] = null;
    }
    return json;
  }

  /// Returns a new [WSSubscribeMessage] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static WSSubscribeMessage? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return WSSubscribeMessage(
        type: WSSubscribeMessageTypeEnum.fromJson(json[r'type']),
        channel: mapValueOfType<String>(json, r'channel'),
        sessionId: mapValueOfType<String>(json, r'session_id'),
      );
    }
    return null;
  }

  static List<WSSubscribeMessage> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <WSSubscribeMessage>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = WSSubscribeMessage.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, WSSubscribeMessage> mapFromJson(dynamic json) {
    final map = <String, WSSubscribeMessage>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = WSSubscribeMessage.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of WSSubscribeMessage-objects as value to a dart map
  static Map<String, List<WSSubscribeMessage>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<WSSubscribeMessage>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = WSSubscribeMessage.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}


class WSSubscribeMessageTypeEnum {
  /// Instantiate a new enum with the provided [value].
  const WSSubscribeMessageTypeEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const subscribe = WSSubscribeMessageTypeEnum._(r'subscribe');

  /// List of all possible values in this [enum][WSSubscribeMessageTypeEnum].
  static const values = <WSSubscribeMessageTypeEnum>[
    subscribe,
  ];

  static WSSubscribeMessageTypeEnum? fromJson(dynamic value) => WSSubscribeMessageTypeEnumTypeTransformer().decode(value);

  static List<WSSubscribeMessageTypeEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <WSSubscribeMessageTypeEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = WSSubscribeMessageTypeEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [WSSubscribeMessageTypeEnum] to String,
/// and [decode] dynamic data back to [WSSubscribeMessageTypeEnum].
class WSSubscribeMessageTypeEnumTypeTransformer {
  factory WSSubscribeMessageTypeEnumTypeTransformer() => _instance ??= const WSSubscribeMessageTypeEnumTypeTransformer._();

  const WSSubscribeMessageTypeEnumTypeTransformer._();

  String encode(WSSubscribeMessageTypeEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a WSSubscribeMessageTypeEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  WSSubscribeMessageTypeEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'subscribe': return WSSubscribeMessageTypeEnum.subscribe;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [WSSubscribeMessageTypeEnumTypeTransformer] instance.
  static WSSubscribeMessageTypeEnumTypeTransformer? _instance;
}


