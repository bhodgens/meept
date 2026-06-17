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

class ShellJobConfig {
  /// Returns a new [ShellJobConfig] instance.
  ShellJobConfig({
    required this.command,
    this.argsCommaOmitempty,
    this.workDirCommaOmitempty,
    this.envCommaOmitempty,
    this.timeoutSecsCommaOmitempty,
    required this.captureOutput,
  });

  String command;

  String? argsCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? workDirCommaOmitempty;

  String? envCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? timeoutSecsCommaOmitempty;

  bool captureOutput;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ShellJobConfig &&
    other.command == command &&
    other.argsCommaOmitempty == argsCommaOmitempty &&
    other.workDirCommaOmitempty == workDirCommaOmitempty &&
    other.envCommaOmitempty == envCommaOmitempty &&
    other.timeoutSecsCommaOmitempty == timeoutSecsCommaOmitempty &&
    other.captureOutput == captureOutput;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (command.hashCode) +
    (argsCommaOmitempty == null ? 0 : argsCommaOmitempty!.hashCode) +
    (workDirCommaOmitempty == null ? 0 : workDirCommaOmitempty!.hashCode) +
    (envCommaOmitempty == null ? 0 : envCommaOmitempty!.hashCode) +
    (timeoutSecsCommaOmitempty == null ? 0 : timeoutSecsCommaOmitempty!.hashCode) +
    (captureOutput.hashCode);

  @override
  String toString() => 'ShellJobConfig[command=$command, argsCommaOmitempty=$argsCommaOmitempty, workDirCommaOmitempty=$workDirCommaOmitempty, envCommaOmitempty=$envCommaOmitempty, timeoutSecsCommaOmitempty=$timeoutSecsCommaOmitempty, captureOutput=$captureOutput]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'command'] = this.command;
    if (this.argsCommaOmitempty != null) {
      json[r'args,omitempty'] = this.argsCommaOmitempty;
    } else {
      json[r'args,omitempty'] = null;
    }
    if (this.workDirCommaOmitempty != null) {
      json[r'work_dir,omitempty'] = this.workDirCommaOmitempty;
    } else {
      json[r'work_dir,omitempty'] = null;
    }
    if (this.envCommaOmitempty != null) {
      json[r'env,omitempty'] = this.envCommaOmitempty;
    } else {
      json[r'env,omitempty'] = null;
    }
    if (this.timeoutSecsCommaOmitempty != null) {
      json[r'timeout_secs,omitempty'] = this.timeoutSecsCommaOmitempty;
    } else {
      json[r'timeout_secs,omitempty'] = null;
    }
      json[r'capture_output'] = this.captureOutput;
    return json;
  }

  /// Returns a new [ShellJobConfig] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ShellJobConfig? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'command'), 'Required key "ShellJobConfig[command]" is missing from JSON.');
        assert(json[r'command'] != null, 'Required key "ShellJobConfig[command]" has a null value in JSON.');
        assert(json.containsKey(r'capture_output'), 'Required key "ShellJobConfig[capture_output]" is missing from JSON.');
        assert(json[r'capture_output'] != null, 'Required key "ShellJobConfig[capture_output]" has a null value in JSON.');
        return true;
      }());

      return ShellJobConfig(
        command: mapValueOfType<String>(json, r'command')!,
        argsCommaOmitempty: mapValueOfType<String>(json, r'args,omitempty'),
        workDirCommaOmitempty: mapValueOfType<String>(json, r'work_dir,omitempty'),
        envCommaOmitempty: mapValueOfType<String>(json, r'env,omitempty'),
        timeoutSecsCommaOmitempty: mapValueOfType<int>(json, r'timeout_secs,omitempty'),
        captureOutput: mapValueOfType<bool>(json, r'capture_output')!,
      );
    }
    return null;
  }

  static List<ShellJobConfig> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ShellJobConfig>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ShellJobConfig.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ShellJobConfig> mapFromJson(dynamic json) {
    final map = <String, ShellJobConfig>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ShellJobConfig.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ShellJobConfig-objects as value to a dart map
  static Map<String, List<ShellJobConfig>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ShellJobConfig>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ShellJobConfig.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'command',
    'capture_output',
  };
}

