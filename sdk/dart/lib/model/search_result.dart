//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class SearchResult {
  /// Returns a new [SearchResult] instance.
  SearchResult({
    required this.type,
    required this.id,
    required this.title,
    required this.snippet,
    required this.relevance,
  });

  String type;

  String id;

  String title;

  String snippet;

  num relevance;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SearchResult &&
    other.type == type &&
    other.id == id &&
    other.title == title &&
    other.snippet == snippet &&
    other.relevance == relevance;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (type.hashCode) +
    (id.hashCode) +
    (title.hashCode) +
    (snippet.hashCode) +
    (relevance.hashCode);

  @override
  String toString() => 'SearchResult[type=$type, id=$id, title=$title, snippet=$snippet, relevance=$relevance]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'type'] = this.type;
      json[r'id'] = this.id;
      json[r'title'] = this.title;
      json[r'snippet'] = this.snippet;
      json[r'relevance'] = this.relevance;
    return json;
  }

  /// Returns a new [SearchResult] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SearchResult? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'type'), 'Required key "SearchResult[type]" is missing from JSON.');
        assert(json[r'type'] != null, 'Required key "SearchResult[type]" has a null value in JSON.');
        assert(json.containsKey(r'id'), 'Required key "SearchResult[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "SearchResult[id]" has a null value in JSON.');
        assert(json.containsKey(r'title'), 'Required key "SearchResult[title]" is missing from JSON.');
        assert(json[r'title'] != null, 'Required key "SearchResult[title]" has a null value in JSON.');
        assert(json.containsKey(r'snippet'), 'Required key "SearchResult[snippet]" is missing from JSON.');
        assert(json[r'snippet'] != null, 'Required key "SearchResult[snippet]" has a null value in JSON.');
        assert(json.containsKey(r'relevance'), 'Required key "SearchResult[relevance]" is missing from JSON.');
        assert(json[r'relevance'] != null, 'Required key "SearchResult[relevance]" has a null value in JSON.');
        return true;
      }());

      return SearchResult(
        type: mapValueOfType<String>(json, r'type')!,
        id: mapValueOfType<String>(json, r'id')!,
        title: mapValueOfType<String>(json, r'title')!,
        snippet: mapValueOfType<String>(json, r'snippet')!,
        relevance: num.parse('${json[r'relevance']}'),
      );
    }
    return null;
  }

  static List<SearchResult> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SearchResult>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SearchResult.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SearchResult> mapFromJson(dynamic json) {
    final map = <String, SearchResult>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SearchResult.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SearchResult-objects as value to a dart map
  static Map<String, List<SearchResult>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SearchResult>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SearchResult.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'type',
    'id',
    'title',
    'snippet',
    'relevance',
  };
}

