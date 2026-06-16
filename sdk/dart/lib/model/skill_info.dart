//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class SkillInfo {
  /// Returns a new [SkillInfo] instance.
  SkillInfo({
    required this.slug,
    required this.name,
    required this.description,
    this.categoryCommaOmitempty,
    this.capabilitiesCommaOmitempty,
    required this.enabled,
    this.uiTypeCommaOmitempty,
  });

  String slug;

  String name;

  String description;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? categoryCommaOmitempty;

  String? capabilitiesCommaOmitempty;

  bool enabled;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? uiTypeCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SkillInfo &&
    other.slug == slug &&
    other.name == name &&
    other.description == description &&
    other.categoryCommaOmitempty == categoryCommaOmitempty &&
    other.capabilitiesCommaOmitempty == capabilitiesCommaOmitempty &&
    other.enabled == enabled &&
    other.uiTypeCommaOmitempty == uiTypeCommaOmitempty;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (slug.hashCode) +
    (name.hashCode) +
    (description.hashCode) +
    (categoryCommaOmitempty == null ? 0 : categoryCommaOmitempty!.hashCode) +
    (capabilitiesCommaOmitempty == null ? 0 : capabilitiesCommaOmitempty!.hashCode) +
    (enabled.hashCode) +
    (uiTypeCommaOmitempty == null ? 0 : uiTypeCommaOmitempty!.hashCode);

  @override
  String toString() => 'SkillInfo[slug=$slug, name=$name, description=$description, categoryCommaOmitempty=$categoryCommaOmitempty, capabilitiesCommaOmitempty=$capabilitiesCommaOmitempty, enabled=$enabled, uiTypeCommaOmitempty=$uiTypeCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'slug'] = this.slug;
      json[r'name'] = this.name;
      json[r'description'] = this.description;
    if (this.categoryCommaOmitempty != null) {
      json[r'category,omitempty'] = this.categoryCommaOmitempty;
    } else {
      json[r'category,omitempty'] = null;
    }
    if (this.capabilitiesCommaOmitempty != null) {
      json[r'capabilities,omitempty'] = this.capabilitiesCommaOmitempty;
    } else {
      json[r'capabilities,omitempty'] = null;
    }
      json[r'enabled'] = this.enabled;
    if (this.uiTypeCommaOmitempty != null) {
      json[r'ui_type,omitempty'] = this.uiTypeCommaOmitempty;
    } else {
      json[r'ui_type,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [SkillInfo] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SkillInfo? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'slug'), 'Required key "SkillInfo[slug]" is missing from JSON.');
        assert(json[r'slug'] != null, 'Required key "SkillInfo[slug]" has a null value in JSON.');
        assert(json.containsKey(r'name'), 'Required key "SkillInfo[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "SkillInfo[name]" has a null value in JSON.');
        assert(json.containsKey(r'description'), 'Required key "SkillInfo[description]" is missing from JSON.');
        assert(json[r'description'] != null, 'Required key "SkillInfo[description]" has a null value in JSON.');
        assert(json.containsKey(r'enabled'), 'Required key "SkillInfo[enabled]" is missing from JSON.');
        assert(json[r'enabled'] != null, 'Required key "SkillInfo[enabled]" has a null value in JSON.');
        return true;
      }());

      return SkillInfo(
        slug: mapValueOfType<String>(json, r'slug')!,
        name: mapValueOfType<String>(json, r'name')!,
        description: mapValueOfType<String>(json, r'description')!,
        categoryCommaOmitempty: mapValueOfType<String>(json, r'category,omitempty'),
        capabilitiesCommaOmitempty: mapValueOfType<String>(json, r'capabilities,omitempty'),
        enabled: mapValueOfType<bool>(json, r'enabled')!,
        uiTypeCommaOmitempty: mapValueOfType<String>(json, r'ui_type,omitempty'),
      );
    }
    return null;
  }

  static List<SkillInfo> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SkillInfo>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SkillInfo.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SkillInfo> mapFromJson(dynamic json) {
    final map = <String, SkillInfo>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SkillInfo.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SkillInfo-objects as value to a dart map
  static Map<String, List<SkillInfo>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SkillInfo>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SkillInfo.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'slug',
    'name',
    'description',
    'enabled',
  };
}

