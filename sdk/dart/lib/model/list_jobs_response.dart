//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ListJobsResponse {
  /// Returns a new [ListJobsResponse] instance.
  ListJobsResponse({
    required this.id,
    required this.name,
    required this.schedule,
    required this.enabled,
    this.lastRunCommaOmitempty,
    this.nextRunCommaOmitempty,
    this.lastErrorCommaOmitempty,
    required this.runCount,
    required this.isRunning,
  });

  String id;

  String name;

  String schedule;

  bool enabled;

  String? lastRunCommaOmitempty;

  String? nextRunCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? lastErrorCommaOmitempty;

  int runCount;

  bool isRunning;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ListJobsResponse &&
    other.id == id &&
    other.name == name &&
    other.schedule == schedule &&
    other.enabled == enabled &&
    other.lastRunCommaOmitempty == lastRunCommaOmitempty &&
    other.nextRunCommaOmitempty == nextRunCommaOmitempty &&
    other.lastErrorCommaOmitempty == lastErrorCommaOmitempty &&
    other.runCount == runCount &&
    other.isRunning == isRunning;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (name.hashCode) +
    (schedule.hashCode) +
    (enabled.hashCode) +
    (lastRunCommaOmitempty == null ? 0 : lastRunCommaOmitempty!.hashCode) +
    (nextRunCommaOmitempty == null ? 0 : nextRunCommaOmitempty!.hashCode) +
    (lastErrorCommaOmitempty == null ? 0 : lastErrorCommaOmitempty!.hashCode) +
    (runCount.hashCode) +
    (isRunning.hashCode);

  @override
  String toString() => 'ListJobsResponse[id=$id, name=$name, schedule=$schedule, enabled=$enabled, lastRunCommaOmitempty=$lastRunCommaOmitempty, nextRunCommaOmitempty=$nextRunCommaOmitempty, lastErrorCommaOmitempty=$lastErrorCommaOmitempty, runCount=$runCount, isRunning=$isRunning]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'name'] = this.name;
      json[r'schedule'] = this.schedule;
      json[r'enabled'] = this.enabled;
    if (this.lastRunCommaOmitempty != null) {
      json[r'last_run,omitempty'] = this.lastRunCommaOmitempty;
    } else {
      json[r'last_run,omitempty'] = null;
    }
    if (this.nextRunCommaOmitempty != null) {
      json[r'next_run,omitempty'] = this.nextRunCommaOmitempty;
    } else {
      json[r'next_run,omitempty'] = null;
    }
    if (this.lastErrorCommaOmitempty != null) {
      json[r'last_error,omitempty'] = this.lastErrorCommaOmitempty;
    } else {
      json[r'last_error,omitempty'] = null;
    }
      json[r'run_count'] = this.runCount;
      json[r'is_running'] = this.isRunning;
    return json;
  }

  /// Returns a new [ListJobsResponse] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ListJobsResponse? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "ListJobsResponse[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "ListJobsResponse[id]" has a null value in JSON.');
        assert(json.containsKey(r'name'), 'Required key "ListJobsResponse[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "ListJobsResponse[name]" has a null value in JSON.');
        assert(json.containsKey(r'schedule'), 'Required key "ListJobsResponse[schedule]" is missing from JSON.');
        assert(json[r'schedule'] != null, 'Required key "ListJobsResponse[schedule]" has a null value in JSON.');
        assert(json.containsKey(r'enabled'), 'Required key "ListJobsResponse[enabled]" is missing from JSON.');
        assert(json[r'enabled'] != null, 'Required key "ListJobsResponse[enabled]" has a null value in JSON.');
        assert(json.containsKey(r'run_count'), 'Required key "ListJobsResponse[run_count]" is missing from JSON.');
        assert(json[r'run_count'] != null, 'Required key "ListJobsResponse[run_count]" has a null value in JSON.');
        assert(json.containsKey(r'is_running'), 'Required key "ListJobsResponse[is_running]" is missing from JSON.');
        assert(json[r'is_running'] != null, 'Required key "ListJobsResponse[is_running]" has a null value in JSON.');
        return true;
      }());

      return ListJobsResponse(
        id: mapValueOfType<String>(json, r'id')!,
        name: mapValueOfType<String>(json, r'name')!,
        schedule: mapValueOfType<String>(json, r'schedule')!,
        enabled: mapValueOfType<bool>(json, r'enabled')!,
        lastRunCommaOmitempty: mapValueOfType<String>(json, r'last_run,omitempty'),
        nextRunCommaOmitempty: mapValueOfType<String>(json, r'next_run,omitempty'),
        lastErrorCommaOmitempty: mapValueOfType<String>(json, r'last_error,omitempty'),
        runCount: mapValueOfType<int>(json, r'run_count')!,
        isRunning: mapValueOfType<bool>(json, r'is_running')!,
      );
    }
    return null;
  }

  static List<ListJobsResponse> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ListJobsResponse>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ListJobsResponse.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ListJobsResponse> mapFromJson(dynamic json) {
    final map = <String, ListJobsResponse>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ListJobsResponse.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ListJobsResponse-objects as value to a dart map
  static Map<String, List<ListJobsResponse>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ListJobsResponse>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ListJobsResponse.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'name',
    'schedule',
    'enabled',
    'run_count',
    'is_running',
  };
}

