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

class TerminalService {
  /// Returns a new [TerminalService] instance.
  TerminalService({
    this.shellTool,
    this.bus,
    this.logger,
    this.history = const [],
    this.historyMu,
    this.maxHistory,
    this.workingDir,
    this.sessionStore,
    this.sessionMu,
  });

  Object? shellTool;

  Object? bus;

  Object? logger;

  List<String>? history;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? historyMu;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? maxHistory;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? workingDir;

  String? sessionStore;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? sessionMu;

  @override
  bool operator ==(Object other) => identical(this, other) || other is TerminalService &&
    other.shellTool == shellTool &&
    other.bus == bus &&
    other.logger == logger &&
    _deepEquality.equals(other.history, history) &&
    other.historyMu == historyMu &&
    other.maxHistory == maxHistory &&
    other.workingDir == workingDir &&
    other.sessionStore == sessionStore &&
    other.sessionMu == sessionMu;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (shellTool == null ? 0 : shellTool!.hashCode) +
    (bus == null ? 0 : bus!.hashCode) +
    (logger == null ? 0 : logger!.hashCode) +
    (history == null ? 0 : history!.hashCode) +
    (historyMu == null ? 0 : historyMu!.hashCode) +
    (maxHistory == null ? 0 : maxHistory!.hashCode) +
    (workingDir == null ? 0 : workingDir!.hashCode) +
    (sessionStore == null ? 0 : sessionStore!.hashCode) +
    (sessionMu == null ? 0 : sessionMu!.hashCode);

  @override
  String toString() => 'TerminalService[shellTool=$shellTool, bus=$bus, logger=$logger, history=$history, historyMu=$historyMu, maxHistory=$maxHistory, workingDir=$workingDir, sessionStore=$sessionStore, sessionMu=$sessionMu]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.shellTool != null) {
      json[r'shellTool'] = this.shellTool;
    } else {
      json[r'shellTool'] = null;
    }
    if (this.bus != null) {
      json[r'bus'] = this.bus;
    } else {
      json[r'bus'] = null;
    }
    if (this.logger != null) {
      json[r'logger'] = this.logger;
    } else {
      json[r'logger'] = null;
    }
    if (this.history != null) {
      json[r'history'] = this.history;
    } else {
      json[r'history'] = null;
    }
    if (this.historyMu != null) {
      json[r'historyMu'] = this.historyMu;
    } else {
      json[r'historyMu'] = null;
    }
    if (this.maxHistory != null) {
      json[r'maxHistory'] = this.maxHistory;
    } else {
      json[r'maxHistory'] = null;
    }
    if (this.workingDir != null) {
      json[r'workingDir'] = this.workingDir;
    } else {
      json[r'workingDir'] = null;
    }
    if (this.sessionStore != null) {
      json[r'sessionStore'] = this.sessionStore;
    } else {
      json[r'sessionStore'] = null;
    }
    if (this.sessionMu != null) {
      json[r'sessionMu'] = this.sessionMu;
    } else {
      json[r'sessionMu'] = null;
    }
    return json;
  }

  /// Returns a new [TerminalService] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static TerminalService? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return TerminalService(
        shellTool: mapValueOfType<Object>(json, r'shellTool'),
        bus: mapValueOfType<Object>(json, r'bus'),
        logger: mapValueOfType<Object>(json, r'logger'),
        history: json[r'history'] is Iterable
            ? (json[r'history'] as Iterable).cast<String>().toList(growable: false)
            : const [],
        historyMu: mapValueOfType<Object>(json, r'historyMu'),
        maxHistory: mapValueOfType<int>(json, r'maxHistory'),
        workingDir: mapValueOfType<String>(json, r'workingDir'),
        sessionStore: mapValueOfType<String>(json, r'sessionStore'),
        sessionMu: mapValueOfType<Object>(json, r'sessionMu'),
      );
    }
    return null;
  }

  static List<TerminalService> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TerminalService>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TerminalService.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, TerminalService> mapFromJson(dynamic json) {
    final map = <String, TerminalService>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = TerminalService.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of TerminalService-objects as value to a dart map
  static Map<String, List<TerminalService>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<TerminalService>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = TerminalService.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

