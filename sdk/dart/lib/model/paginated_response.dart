//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class PaginatedResponse {
  /// Returns a new [PaginatedResponse] instance.
  PaginatedResponse({
    this.items = const [],
    required this.total,
    required this.hasMore,
    this.nextOffsetCommaOmitempty,
  });

  List<String>? items;

  int total;

  bool hasMore;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? nextOffsetCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is PaginatedResponse &&
    _deepEquality.equals(other.items, items) &&
    other.total == total &&
    other.hasMore == hasMore &&
    other.nextOffsetCommaOmitempty == nextOffsetCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (items == null ? 0 : items!.hashCode) +
    (total.hashCode) +
    (hasMore.hashCode) +
    (nextOffsetCommaOmitempty == null ? 0 : nextOffsetCommaOmitempty!.hashCode);

  @override
  String toString() => 'PaginatedResponse[items=$items, total=$total, hasMore=$hasMore, nextOffsetCommaOmitempty=$nextOffsetCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.items != null) {
      json[r'items'] = this.items;
    } else {
      json[r'items'] = null;
    }
      json[r'total'] = this.total;
      json[r'has_more'] = this.hasMore;
    if (this.nextOffsetCommaOmitempty != null) {
      json[r'next_offset,omitempty'] = this.nextOffsetCommaOmitempty;
    } else {
      json[r'next_offset,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [PaginatedResponse] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static PaginatedResponse? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'items'), 'Required key "PaginatedResponse[items]" is missing from JSON.');
        assert(json.containsKey(r'total'), 'Required key "PaginatedResponse[total]" is missing from JSON.');
        assert(json[r'total'] != null, 'Required key "PaginatedResponse[total]" has a null value in JSON.');
        assert(json.containsKey(r'has_more'), 'Required key "PaginatedResponse[has_more]" is missing from JSON.');
        assert(json[r'has_more'] != null, 'Required key "PaginatedResponse[has_more]" has a null value in JSON.');
        return true;
      }());

      return PaginatedResponse(
        items: json[r'items'] is Iterable
            ? (json[r'items'] as Iterable).cast<String>().toList(growable: false)
            : const [],
        total: mapValueOfType<int>(json, r'total')!,
        hasMore: mapValueOfType<bool>(json, r'has_more')!,
        nextOffsetCommaOmitempty: mapValueOfType<int>(json, r'next_offset,omitempty'),
      );
    }
    return null;
  }

  static List<PaginatedResponse> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <PaginatedResponse>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = PaginatedResponse.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, PaginatedResponse> mapFromJson(dynamic json) {
    final map = <String, PaginatedResponse>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = PaginatedResponse.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of PaginatedResponse-objects as value to a dart map
  static Map<String, List<PaginatedResponse>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<PaginatedResponse>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = PaginatedResponse.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'items',
    'total',
    'has_more',
  };
}

