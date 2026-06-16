//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class TaskService {
  /// Returns a new [TaskService] instance.
  TaskService({
    this.registry,
  });

  Object? registry;

  @override
  bool operator ==(Object other) => identical(this, other) || other is TaskService &&
    other.registry == registry;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (registry == null ? 0 : registry!.hashCode);

  @override
  String toString() => 'TaskService[registry=$registry]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.registry != null) {
      json[r'registry'] = this.registry;
    } else {
      json[r'registry'] = null;
    }
    return json;
  }

  /// Returns a new [TaskService] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static TaskService? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return TaskService(
        registry: mapValueOfType<Object>(json, r'registry'),
      );
    }
    return null;
  }

  static List<TaskService> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TaskService>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TaskService.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, TaskService> mapFromJson(dynamic json) {
    final map = <String, TaskService>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = TaskService.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of TaskService-objects as value to a dart map
  static Map<String, List<TaskService>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<TaskService>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = TaskService.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

