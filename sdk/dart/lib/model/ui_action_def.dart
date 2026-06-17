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

class UIActionDef {
  /// Returns a new [UIActionDef] instance.
  UIActionDef({
    required this.id,
    required this.label,
    required this.type,
    this.styleCommaOmitempty,
  });

  String id;

  String label;

  String type;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? styleCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is UIActionDef &&
    other.id == id &&
    other.label == label &&
    other.type == type &&
    other.styleCommaOmitempty == styleCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (label.hashCode) +
    (type.hashCode) +
    (styleCommaOmitempty == null ? 0 : styleCommaOmitempty!.hashCode);

  @override
  String toString() => 'UIActionDef[id=$id, label=$label, type=$type, styleCommaOmitempty=$styleCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'label'] = this.label;
      json[r'type'] = this.type;
    if (this.styleCommaOmitempty != null) {
      json[r'style,omitempty'] = this.styleCommaOmitempty;
    } else {
      json[r'style,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [UIActionDef] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static UIActionDef? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "UIActionDef[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "UIActionDef[id]" has a null value in JSON.');
        assert(json.containsKey(r'label'), 'Required key "UIActionDef[label]" is missing from JSON.');
        assert(json[r'label'] != null, 'Required key "UIActionDef[label]" has a null value in JSON.');
        assert(json.containsKey(r'type'), 'Required key "UIActionDef[type]" is missing from JSON.');
        assert(json[r'type'] != null, 'Required key "UIActionDef[type]" has a null value in JSON.');
        return true;
      }());

      return UIActionDef(
        id: mapValueOfType<String>(json, r'id')!,
        label: mapValueOfType<String>(json, r'label')!,
        type: mapValueOfType<String>(json, r'type')!,
        styleCommaOmitempty: mapValueOfType<String>(json, r'style,omitempty'),
      );
    }
    return null;
  }

  static List<UIActionDef> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <UIActionDef>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = UIActionDef.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, UIActionDef> mapFromJson(dynamic json) {
    final map = <String, UIActionDef>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = UIActionDef.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of UIActionDef-objects as value to a dart map
  static Map<String, List<UIActionDef>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<UIActionDef>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = UIActionDef.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'label',
    'type',
  };
}

