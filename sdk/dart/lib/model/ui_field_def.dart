//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class UIFieldDef {
  /// Returns a new [UIFieldDef] instance.
  UIFieldDef({
    required this.name,
    required this.label,
    required this.type,
    this.requiredCommaOmitempty,
    this.defaultCommaOmitempty,
    this.optionsCommaOmitempty,
    this.placeholderCommaOmitempty,
    this.helpCommaOmitempty,
  });

  String name;

  String label;

  String type;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  bool? requiredCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? defaultCommaOmitempty;

  String? optionsCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? placeholderCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? helpCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is UIFieldDef &&
    other.name == name &&
    other.label == label &&
    other.type == type &&
    other.requiredCommaOmitempty == requiredCommaOmitempty &&
    other.defaultCommaOmitempty == defaultCommaOmitempty &&
    other.optionsCommaOmitempty == optionsCommaOmitempty &&
    other.placeholderCommaOmitempty == placeholderCommaOmitempty &&
    other.helpCommaOmitempty == helpCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (name.hashCode) +
    (label.hashCode) +
    (type.hashCode) +
    (requiredCommaOmitempty == null ? 0 : requiredCommaOmitempty!.hashCode) +
    (defaultCommaOmitempty == null ? 0 : defaultCommaOmitempty!.hashCode) +
    (optionsCommaOmitempty == null ? 0 : optionsCommaOmitempty!.hashCode) +
    (placeholderCommaOmitempty == null ? 0 : placeholderCommaOmitempty!.hashCode) +
    (helpCommaOmitempty == null ? 0 : helpCommaOmitempty!.hashCode);

  @override
  String toString() => 'UIFieldDef[name=$name, label=$label, type=$type, requiredCommaOmitempty=$requiredCommaOmitempty, defaultCommaOmitempty=$defaultCommaOmitempty, optionsCommaOmitempty=$optionsCommaOmitempty, placeholderCommaOmitempty=$placeholderCommaOmitempty, helpCommaOmitempty=$helpCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'name'] = this.name;
      json[r'label'] = this.label;
      json[r'type'] = this.type;
    if (this.requiredCommaOmitempty != null) {
      json[r'required,omitempty'] = this.requiredCommaOmitempty;
    } else {
      json[r'required,omitempty'] = null;
    }
    if (this.defaultCommaOmitempty != null) {
      json[r'default,omitempty'] = this.defaultCommaOmitempty;
    } else {
      json[r'default,omitempty'] = null;
    }
    if (this.optionsCommaOmitempty != null) {
      json[r'options,omitempty'] = this.optionsCommaOmitempty;
    } else {
      json[r'options,omitempty'] = null;
    }
    if (this.placeholderCommaOmitempty != null) {
      json[r'placeholder,omitempty'] = this.placeholderCommaOmitempty;
    } else {
      json[r'placeholder,omitempty'] = null;
    }
    if (this.helpCommaOmitempty != null) {
      json[r'help,omitempty'] = this.helpCommaOmitempty;
    } else {
      json[r'help,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [UIFieldDef] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static UIFieldDef? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'name'), 'Required key "UIFieldDef[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "UIFieldDef[name]" has a null value in JSON.');
        assert(json.containsKey(r'label'), 'Required key "UIFieldDef[label]" is missing from JSON.');
        assert(json[r'label'] != null, 'Required key "UIFieldDef[label]" has a null value in JSON.');
        assert(json.containsKey(r'type'), 'Required key "UIFieldDef[type]" is missing from JSON.');
        assert(json[r'type'] != null, 'Required key "UIFieldDef[type]" has a null value in JSON.');
        return true;
      }());

      return UIFieldDef(
        name: mapValueOfType<String>(json, r'name')!,
        label: mapValueOfType<String>(json, r'label')!,
        type: mapValueOfType<String>(json, r'type')!,
        requiredCommaOmitempty: mapValueOfType<bool>(json, r'required,omitempty'),
        defaultCommaOmitempty: mapValueOfType<Object>(json, r'default,omitempty'),
        optionsCommaOmitempty: mapValueOfType<String>(json, r'options,omitempty'),
        placeholderCommaOmitempty: mapValueOfType<String>(json, r'placeholder,omitempty'),
        helpCommaOmitempty: mapValueOfType<String>(json, r'help,omitempty'),
      );
    }
    return null;
  }

  static List<UIFieldDef> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <UIFieldDef>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = UIFieldDef.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, UIFieldDef> mapFromJson(dynamic json) {
    final map = <String, UIFieldDef>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = UIFieldDef.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of UIFieldDef-objects as value to a dart map
  static Map<String, List<UIFieldDef>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<UIFieldDef>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = UIFieldDef.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'name',
    'label',
    'type',
  };
}

