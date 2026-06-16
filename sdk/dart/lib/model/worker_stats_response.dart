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

class WorkerStatsResponse {
  /// Returns a new [WorkerStatsResponse] instance.
  WorkerStatsResponse({
    required this.totalWorkers,
    required this.idleWorkers,
    required this.busyWorkers,
    required this.errorWorkers,
    this.workerStats = const [],
  });

  int totalWorkers;

  int idleWorkers;

  int busyWorkers;

  int errorWorkers;

  List<String>? workerStats;

  @override
  bool operator ==(Object other) => identical(this, other) || other is WorkerStatsResponse &&
    other.totalWorkers == totalWorkers &&
    other.idleWorkers == idleWorkers &&
    other.busyWorkers == busyWorkers &&
    other.errorWorkers == errorWorkers &&
    _deepEquality.equals(other.workerStats, workerStats);

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (totalWorkers.hashCode) +
    (idleWorkers.hashCode) +
    (busyWorkers.hashCode) +
    (errorWorkers.hashCode) +
    (workerStats == null ? 0 : workerStats!.hashCode);

  @override
  String toString() => 'WorkerStatsResponse[totalWorkers=$totalWorkers, idleWorkers=$idleWorkers, busyWorkers=$busyWorkers, errorWorkers=$errorWorkers, workerStats=$workerStats]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'total_workers'] = this.totalWorkers;
      json[r'idle_workers'] = this.idleWorkers;
      json[r'busy_workers'] = this.busyWorkers;
      json[r'error_workers'] = this.errorWorkers;
    if (this.workerStats != null) {
      json[r'worker_stats'] = this.workerStats;
    } else {
      json[r'worker_stats'] = null;
    }
    return json;
  }

  /// Returns a new [WorkerStatsResponse] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static WorkerStatsResponse? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'total_workers'), 'Required key "WorkerStatsResponse[total_workers]" is missing from JSON.');
        assert(json[r'total_workers'] != null, 'Required key "WorkerStatsResponse[total_workers]" has a null value in JSON.');
        assert(json.containsKey(r'idle_workers'), 'Required key "WorkerStatsResponse[idle_workers]" is missing from JSON.');
        assert(json[r'idle_workers'] != null, 'Required key "WorkerStatsResponse[idle_workers]" has a null value in JSON.');
        assert(json.containsKey(r'busy_workers'), 'Required key "WorkerStatsResponse[busy_workers]" is missing from JSON.');
        assert(json[r'busy_workers'] != null, 'Required key "WorkerStatsResponse[busy_workers]" has a null value in JSON.');
        assert(json.containsKey(r'error_workers'), 'Required key "WorkerStatsResponse[error_workers]" is missing from JSON.');
        assert(json[r'error_workers'] != null, 'Required key "WorkerStatsResponse[error_workers]" has a null value in JSON.');
        assert(json.containsKey(r'worker_stats'), 'Required key "WorkerStatsResponse[worker_stats]" is missing from JSON.');
        return true;
      }());

      return WorkerStatsResponse(
        totalWorkers: mapValueOfType<int>(json, r'total_workers')!,
        idleWorkers: mapValueOfType<int>(json, r'idle_workers')!,
        busyWorkers: mapValueOfType<int>(json, r'busy_workers')!,
        errorWorkers: mapValueOfType<int>(json, r'error_workers')!,
        workerStats: json[r'worker_stats'] is Iterable
            ? (json[r'worker_stats'] as Iterable).cast<String>().toList(growable: false)
            : const [],
      );
    }
    return null;
  }

  static List<WorkerStatsResponse> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <WorkerStatsResponse>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = WorkerStatsResponse.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, WorkerStatsResponse> mapFromJson(dynamic json) {
    final map = <String, WorkerStatsResponse>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = WorkerStatsResponse.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of WorkerStatsResponse-objects as value to a dart map
  static Map<String, List<WorkerStatsResponse>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<WorkerStatsResponse>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = WorkerStatsResponse.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'total_workers',
    'idle_workers',
    'busy_workers',
    'error_workers',
    'worker_stats',
  };
}

