//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class SearchService {
  /// Returns a new [SearchService] instance.
  SearchService({
    this.sessionStore,
    this.taskRegistry,
    this.memoryMgr,
    this.planStore,
  });

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? sessionStore;

  Object? taskRegistry;

  Object? memoryMgr;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? planStore;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SearchService &&
    other.sessionStore == sessionStore &&
    other.taskRegistry == taskRegistry &&
    other.memoryMgr == memoryMgr &&
    other.planStore == planStore;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (sessionStore == null ? 0 : sessionStore!.hashCode) +
    (taskRegistry == null ? 0 : taskRegistry!.hashCode) +
    (memoryMgr == null ? 0 : memoryMgr!.hashCode) +
    (planStore == null ? 0 : planStore!.hashCode);

  @override
  String toString() => 'SearchService[sessionStore=$sessionStore, taskRegistry=$taskRegistry, memoryMgr=$memoryMgr, planStore=$planStore]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.sessionStore != null) {
      json[r'sessionStore'] = this.sessionStore;
    } else {
      json[r'sessionStore'] = null;
    }
    if (this.taskRegistry != null) {
      json[r'taskRegistry'] = this.taskRegistry;
    } else {
      json[r'taskRegistry'] = null;
    }
    if (this.memoryMgr != null) {
      json[r'memoryMgr'] = this.memoryMgr;
    } else {
      json[r'memoryMgr'] = null;
    }
    if (this.planStore != null) {
      json[r'planStore'] = this.planStore;
    } else {
      json[r'planStore'] = null;
    }
    return json;
  }

  /// Returns a new [SearchService] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SearchService? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return SearchService(
        sessionStore: mapValueOfType<Object>(json, r'sessionStore'),
        taskRegistry: mapValueOfType<Object>(json, r'taskRegistry'),
        memoryMgr: mapValueOfType<Object>(json, r'memoryMgr'),
        planStore: mapValueOfType<Object>(json, r'planStore'),
      );
    }
    return null;
  }

  static List<SearchService> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SearchService>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SearchService.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SearchService> mapFromJson(dynamic json) {
    final map = <String, SearchService>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SearchService.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SearchService-objects as value to a dart map
  static Map<String, List<SearchService>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SearchService>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SearchService.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

