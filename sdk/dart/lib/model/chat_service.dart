//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ChatService {
  /// Returns a new [ChatService] instance.
  ChatService({
    this.bus,
    this.agentRegistry,
    this.sessionStore,
    this.logger,
  });

  Object? bus;

  Object? agentRegistry;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? sessionStore;

  Object? logger;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ChatService &&
    other.bus == bus &&
    other.agentRegistry == agentRegistry &&
    other.sessionStore == sessionStore &&
    other.logger == logger;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (bus == null ? 0 : bus!.hashCode) +
    (agentRegistry == null ? 0 : agentRegistry!.hashCode) +
    (sessionStore == null ? 0 : sessionStore!.hashCode) +
    (logger == null ? 0 : logger!.hashCode);

  @override
  String toString() => 'ChatService[bus=$bus, agentRegistry=$agentRegistry, sessionStore=$sessionStore, logger=$logger]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.bus != null) {
      json[r'bus'] = this.bus;
    } else {
      json[r'bus'] = null;
    }
    if (this.agentRegistry != null) {
      json[r'agentRegistry'] = this.agentRegistry;
    } else {
      json[r'agentRegistry'] = null;
    }
    if (this.sessionStore != null) {
      json[r'sessionStore'] = this.sessionStore;
    } else {
      json[r'sessionStore'] = null;
    }
    if (this.logger != null) {
      json[r'logger'] = this.logger;
    } else {
      json[r'logger'] = null;
    }
    return json;
  }

  /// Returns a new [ChatService] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ChatService? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return ChatService(
        bus: mapValueOfType<Object>(json, r'bus'),
        agentRegistry: mapValueOfType<Object>(json, r'agentRegistry'),
        sessionStore: mapValueOfType<Object>(json, r'sessionStore'),
        logger: mapValueOfType<Object>(json, r'logger'),
      );
    }
    return null;
  }

  static List<ChatService> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ChatService>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ChatService.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ChatService> mapFromJson(dynamic json) {
    final map = <String, ChatService>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ChatService.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ChatService-objects as value to a dart map
  static Map<String, List<ChatService>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ChatService>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ChatService.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

