//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class BranchSessionRequest {
  /// Returns a new [BranchSessionRequest] instance.
  BranchSessionRequest({
    required this.id,
    required this.targetMessageId,
  });

  String id;

  int targetMessageId;

  @override
  bool operator ==(Object other) => identical(this, other) || other is BranchSessionRequest &&
    other.id == id &&
    other.targetMessageId == targetMessageId;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (targetMessageId.hashCode);

  @override
  String toString() => 'BranchSessionRequest[id=$id, targetMessageId=$targetMessageId]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'target_message_id'] = this.targetMessageId;
    return json;
  }

  /// Returns a new [BranchSessionRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static BranchSessionRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "BranchSessionRequest[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "BranchSessionRequest[id]" has a null value in JSON.');
        assert(json.containsKey(r'target_message_id'), 'Required key "BranchSessionRequest[target_message_id]" is missing from JSON.');
        assert(json[r'target_message_id'] != null, 'Required key "BranchSessionRequest[target_message_id]" has a null value in JSON.');
        return true;
      }());

      return BranchSessionRequest(
        id: mapValueOfType<String>(json, r'id')!,
        targetMessageId: mapValueOfType<int>(json, r'target_message_id')!,
      );
    }
    return null;
  }

  static List<BranchSessionRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <BranchSessionRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = BranchSessionRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, BranchSessionRequest> mapFromJson(dynamic json) {
    final map = <String, BranchSessionRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = BranchSessionRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of BranchSessionRequest-objects as value to a dart map
  static Map<String, List<BranchSessionRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<BranchSessionRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = BranchSessionRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'target_message_id',
  };
}

