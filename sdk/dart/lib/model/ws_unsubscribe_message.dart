//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class WSUnsubscribeMessage {
  /// Returns a new [WSUnsubscribeMessage] instance.
  WSUnsubscribeMessage({
    this.type,
    this.channel,
    this.sessionId,
  });

  WSUnsubscribeMessageTypeEnum? type;

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
  bool operator ==(Object other) => identical(this, other) || other is WSUnsubscribeMessage &&
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
  String toString() => 'WSUnsubscribeMessage[type=$type, channel=$channel, sessionId=$sessionId]';

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

  /// Returns a new [WSUnsubscribeMessage] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static WSUnsubscribeMessage? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return WSUnsubscribeMessage(
        type: WSUnsubscribeMessageTypeEnum.fromJson(json[r'type']),
        channel: mapValueOfType<String>(json, r'channel'),
        sessionId: mapValueOfType<String>(json, r'session_id'),
      );
    }
    return null;
  }

  static List<WSUnsubscribeMessage> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <WSUnsubscribeMessage>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = WSUnsubscribeMessage.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, WSUnsubscribeMessage> mapFromJson(dynamic json) {
    final map = <String, WSUnsubscribeMessage>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = WSUnsubscribeMessage.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of WSUnsubscribeMessage-objects as value to a dart map
  static Map<String, List<WSUnsubscribeMessage>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<WSUnsubscribeMessage>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = WSUnsubscribeMessage.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}


class WSUnsubscribeMessageTypeEnum {
  /// Instantiate a new enum with the provided [value].
  const WSUnsubscribeMessageTypeEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const unsubscribe = WSUnsubscribeMessageTypeEnum._(r'unsubscribe');

  /// List of all possible values in this [enum][WSUnsubscribeMessageTypeEnum].
  static const values = <WSUnsubscribeMessageTypeEnum>[
    unsubscribe,
  ];

  static WSUnsubscribeMessageTypeEnum? fromJson(dynamic value) => WSUnsubscribeMessageTypeEnumTypeTransformer().decode(value);

  static List<WSUnsubscribeMessageTypeEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <WSUnsubscribeMessageTypeEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = WSUnsubscribeMessageTypeEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [WSUnsubscribeMessageTypeEnum] to String,
/// and [decode] dynamic data back to [WSUnsubscribeMessageTypeEnum].
class WSUnsubscribeMessageTypeEnumTypeTransformer {
  factory WSUnsubscribeMessageTypeEnumTypeTransformer() => _instance ??= const WSUnsubscribeMessageTypeEnumTypeTransformer._();

  const WSUnsubscribeMessageTypeEnumTypeTransformer._();

  String encode(WSUnsubscribeMessageTypeEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a WSUnsubscribeMessageTypeEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  WSUnsubscribeMessageTypeEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'unsubscribe': return WSUnsubscribeMessageTypeEnum.unsubscribe;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [WSUnsubscribeMessageTypeEnumTypeTransformer] instance.
  static WSUnsubscribeMessageTypeEnumTypeTransformer? _instance;
}


