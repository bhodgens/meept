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

class SearchRequest {
  /// Returns a new [SearchRequest] instance.
  SearchRequest({
    required this.query,
    this.scopeCommaOmitempty,
    this.limitCommaOmitempty,
  });

  String query;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? scopeCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? limitCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SearchRequest &&
    other.query == query &&
    other.scopeCommaOmitempty == scopeCommaOmitempty &&
    other.limitCommaOmitempty == limitCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (query.hashCode) +
    (scopeCommaOmitempty == null ? 0 : scopeCommaOmitempty!.hashCode) +
    (limitCommaOmitempty == null ? 0 : limitCommaOmitempty!.hashCode);

  @override
  String toString() => 'SearchRequest[query=$query, scopeCommaOmitempty=$scopeCommaOmitempty, limitCommaOmitempty=$limitCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'query'] = this.query;
    if (this.scopeCommaOmitempty != null) {
      json[r'scope,omitempty'] = this.scopeCommaOmitempty;
    } else {
      json[r'scope,omitempty'] = null;
    }
    if (this.limitCommaOmitempty != null) {
      json[r'limit,omitempty'] = this.limitCommaOmitempty;
    } else {
      json[r'limit,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [SearchRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SearchRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'query'), 'Required key "SearchRequest[query]" is missing from JSON.');
        assert(json[r'query'] != null, 'Required key "SearchRequest[query]" has a null value in JSON.');
        return true;
      }());

      return SearchRequest(
        query: mapValueOfType<String>(json, r'query')!,
        scopeCommaOmitempty: mapValueOfType<String>(json, r'scope,omitempty'),
        limitCommaOmitempty: mapValueOfType<int>(json, r'limit,omitempty'),
      );
    }
    return null;
  }

  static List<SearchRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SearchRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SearchRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SearchRequest> mapFromJson(dynamic json) {
    final map = <String, SearchRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SearchRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SearchRequest-objects as value to a dart map
  static Map<String, List<SearchRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SearchRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SearchRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'query',
  };
}

