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

class CommandHistory {
  /// Returns a new [CommandHistory] instance.
  CommandHistory({
    required this.id,
    required this.command,
    this.outputCommaOmitempty,
    this.stderrCommaOmitempty,
    required this.exitCode,
    required this.timestamp,
    required this.workingDir,
    required this.durationMs,
    required this.riskLevel,
    required this.success,
  });

  String id;

  String command;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? outputCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? stderrCommaOmitempty;

  int exitCode;

  String timestamp;

  String workingDir;

  Object durationMs;

  Object riskLevel;

  bool success;

  @override
  bool operator ==(Object other) => identical(this, other) || other is CommandHistory &&
    other.id == id &&
    other.command == command &&
    other.outputCommaOmitempty == outputCommaOmitempty &&
    other.stderrCommaOmitempty == stderrCommaOmitempty &&
    other.exitCode == exitCode &&
    other.timestamp == timestamp &&
    other.workingDir == workingDir &&
    other.durationMs == durationMs &&
    other.riskLevel == riskLevel &&
    other.success == success;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (command.hashCode) +
    (outputCommaOmitempty == null ? 0 : outputCommaOmitempty!.hashCode) +
    (stderrCommaOmitempty == null ? 0 : stderrCommaOmitempty!.hashCode) +
    (exitCode.hashCode) +
    (timestamp.hashCode) +
    (workingDir.hashCode) +
    (durationMs.hashCode) +
    (riskLevel.hashCode) +
    (success.hashCode);

  @override
  String toString() => 'CommandHistory[id=$id, command=$command, outputCommaOmitempty=$outputCommaOmitempty, stderrCommaOmitempty=$stderrCommaOmitempty, exitCode=$exitCode, timestamp=$timestamp, workingDir=$workingDir, durationMs=$durationMs, riskLevel=$riskLevel, success=$success]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'command'] = this.command;
    if (this.outputCommaOmitempty != null) {
      json[r'output,omitempty'] = this.outputCommaOmitempty;
    } else {
      json[r'output,omitempty'] = null;
    }
    if (this.stderrCommaOmitempty != null) {
      json[r'stderr,omitempty'] = this.stderrCommaOmitempty;
    } else {
      json[r'stderr,omitempty'] = null;
    }
      json[r'exit_code'] = this.exitCode;
      json[r'timestamp'] = this.timestamp;
      json[r'working_dir'] = this.workingDir;
      json[r'duration_ms'] = this.durationMs;
      json[r'risk_level'] = this.riskLevel;
      json[r'success'] = this.success;
    return json;
  }

  /// Returns a new [CommandHistory] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static CommandHistory? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "CommandHistory[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "CommandHistory[id]" has a null value in JSON.');
        assert(json.containsKey(r'command'), 'Required key "CommandHistory[command]" is missing from JSON.');
        assert(json[r'command'] != null, 'Required key "CommandHistory[command]" has a null value in JSON.');
        assert(json.containsKey(r'exit_code'), 'Required key "CommandHistory[exit_code]" is missing from JSON.');
        assert(json[r'exit_code'] != null, 'Required key "CommandHistory[exit_code]" has a null value in JSON.');
        assert(json.containsKey(r'timestamp'), 'Required key "CommandHistory[timestamp]" is missing from JSON.');
        assert(json[r'timestamp'] != null, 'Required key "CommandHistory[timestamp]" has a null value in JSON.');
        assert(json.containsKey(r'working_dir'), 'Required key "CommandHistory[working_dir]" is missing from JSON.');
        assert(json[r'working_dir'] != null, 'Required key "CommandHistory[working_dir]" has a null value in JSON.');
        assert(json.containsKey(r'duration_ms'), 'Required key "CommandHistory[duration_ms]" is missing from JSON.');
        assert(json[r'duration_ms'] != null, 'Required key "CommandHistory[duration_ms]" has a null value in JSON.');
        assert(json.containsKey(r'risk_level'), 'Required key "CommandHistory[risk_level]" is missing from JSON.');
        assert(json[r'risk_level'] != null, 'Required key "CommandHistory[risk_level]" has a null value in JSON.');
        assert(json.containsKey(r'success'), 'Required key "CommandHistory[success]" is missing from JSON.');
        assert(json[r'success'] != null, 'Required key "CommandHistory[success]" has a null value in JSON.');
        return true;
      }());

      return CommandHistory(
        id: mapValueOfType<String>(json, r'id')!,
        command: mapValueOfType<String>(json, r'command')!,
        outputCommaOmitempty: mapValueOfType<String>(json, r'output,omitempty'),
        stderrCommaOmitempty: mapValueOfType<String>(json, r'stderr,omitempty'),
        exitCode: mapValueOfType<int>(json, r'exit_code')!,
        timestamp: mapValueOfType<String>(json, r'timestamp')!,
        workingDir: mapValueOfType<String>(json, r'working_dir')!,
        durationMs: mapValueOfType<Object>(json, r'duration_ms')!,
        riskLevel: mapValueOfType<Object>(json, r'risk_level')!,
        success: mapValueOfType<bool>(json, r'success')!,
      );
    }
    return null;
  }

  static List<CommandHistory> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <CommandHistory>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = CommandHistory.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, CommandHistory> mapFromJson(dynamic json) {
    final map = <String, CommandHistory>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = CommandHistory.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of CommandHistory-objects as value to a dart map
  static Map<String, List<CommandHistory>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<CommandHistory>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = CommandHistory.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'command',
    'exit_code',
    'timestamp',
    'working_dir',
    'duration_ms',
    'risk_level',
    'success',
  };
}

