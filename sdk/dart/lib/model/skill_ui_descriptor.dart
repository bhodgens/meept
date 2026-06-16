//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class SkillUIDescriptor {
  /// Returns a new [SkillUIDescriptor] instance.
  SkillUIDescriptor({
    required this.slug,
    required this.name,
    required this.description,
    required this.uiType,
    this.categoryCommaOmitempty,
    this.tagsCommaOmitempty,
    this.examplesCommaOmitempty,
    this.riskLevelCommaOmitempty,
    this.bodyCommaOmitempty,
    this.fieldsCommaOmitempty = const [],
    this.actionsCommaOmitempty = const [],
  });

  String slug;

  String name;

  String description;

  String uiType;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? categoryCommaOmitempty;

  String? tagsCommaOmitempty;

  String? examplesCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? riskLevelCommaOmitempty;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? bodyCommaOmitempty;

  List<String>? fieldsCommaOmitempty;

  List<String>? actionsCommaOmitempty;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SkillUIDescriptor &&
    other.slug == slug &&
    other.name == name &&
    other.description == description &&
    other.uiType == uiType &&
    other.categoryCommaOmitempty == categoryCommaOmitempty &&
    other.tagsCommaOmitempty == tagsCommaOmitempty &&
    other.examplesCommaOmitempty == examplesCommaOmitempty &&
    other.riskLevelCommaOmitempty == riskLevelCommaOmitempty &&
    other.bodyCommaOmitempty == bodyCommaOmitempty &&
    _deepEquality.equals(other.fieldsCommaOmitempty, fieldsCommaOmitempty) &&
    _deepEquality.equals(other.actionsCommaOmitempty, actionsCommaOmitempty);

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (slug.hashCode) +
    (name.hashCode) +
    (description.hashCode) +
    (uiType.hashCode) +
    (categoryCommaOmitempty == null ? 0 : categoryCommaOmitempty!.hashCode) +
    (tagsCommaOmitempty == null ? 0 : tagsCommaOmitempty!.hashCode) +
    (examplesCommaOmitempty == null ? 0 : examplesCommaOmitempty!.hashCode) +
    (riskLevelCommaOmitempty == null ? 0 : riskLevelCommaOmitempty!.hashCode) +
    (bodyCommaOmitempty == null ? 0 : bodyCommaOmitempty!.hashCode) +
    (fieldsCommaOmitempty == null ? 0 : fieldsCommaOmitempty!.hashCode) +
    (actionsCommaOmitempty == null ? 0 : actionsCommaOmitempty!.hashCode);

  @override
  String toString() => 'SkillUIDescriptor[slug=$slug, name=$name, description=$description, uiType=$uiType, categoryCommaOmitempty=$categoryCommaOmitempty, tagsCommaOmitempty=$tagsCommaOmitempty, examplesCommaOmitempty=$examplesCommaOmitempty, riskLevelCommaOmitempty=$riskLevelCommaOmitempty, bodyCommaOmitempty=$bodyCommaOmitempty, fieldsCommaOmitempty=$fieldsCommaOmitempty, actionsCommaOmitempty=$actionsCommaOmitempty]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'slug'] = this.slug;
      json[r'name'] = this.name;
      json[r'description'] = this.description;
      json[r'ui_type'] = this.uiType;
    if (this.categoryCommaOmitempty != null) {
      json[r'category,omitempty'] = this.categoryCommaOmitempty;
    } else {
      json[r'category,omitempty'] = null;
    }
    if (this.tagsCommaOmitempty != null) {
      json[r'tags,omitempty'] = this.tagsCommaOmitempty;
    } else {
      json[r'tags,omitempty'] = null;
    }
    if (this.examplesCommaOmitempty != null) {
      json[r'examples,omitempty'] = this.examplesCommaOmitempty;
    } else {
      json[r'examples,omitempty'] = null;
    }
    if (this.riskLevelCommaOmitempty != null) {
      json[r'risk_level,omitempty'] = this.riskLevelCommaOmitempty;
    } else {
      json[r'risk_level,omitempty'] = null;
    }
    if (this.bodyCommaOmitempty != null) {
      json[r'body,omitempty'] = this.bodyCommaOmitempty;
    } else {
      json[r'body,omitempty'] = null;
    }
    if (this.fieldsCommaOmitempty != null) {
      json[r'fields,omitempty'] = this.fieldsCommaOmitempty;
    } else {
      json[r'fields,omitempty'] = null;
    }
    if (this.actionsCommaOmitempty != null) {
      json[r'actions,omitempty'] = this.actionsCommaOmitempty;
    } else {
      json[r'actions,omitempty'] = null;
    }
    return json;
  }

  /// Returns a new [SkillUIDescriptor] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SkillUIDescriptor? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'slug'), 'Required key "SkillUIDescriptor[slug]" is missing from JSON.');
        assert(json[r'slug'] != null, 'Required key "SkillUIDescriptor[slug]" has a null value in JSON.');
        assert(json.containsKey(r'name'), 'Required key "SkillUIDescriptor[name]" is missing from JSON.');
        assert(json[r'name'] != null, 'Required key "SkillUIDescriptor[name]" has a null value in JSON.');
        assert(json.containsKey(r'description'), 'Required key "SkillUIDescriptor[description]" is missing from JSON.');
        assert(json[r'description'] != null, 'Required key "SkillUIDescriptor[description]" has a null value in JSON.');
        assert(json.containsKey(r'ui_type'), 'Required key "SkillUIDescriptor[ui_type]" is missing from JSON.');
        assert(json[r'ui_type'] != null, 'Required key "SkillUIDescriptor[ui_type]" has a null value in JSON.');
        return true;
      }());

      return SkillUIDescriptor(
        slug: mapValueOfType<String>(json, r'slug')!,
        name: mapValueOfType<String>(json, r'name')!,
        description: mapValueOfType<String>(json, r'description')!,
        uiType: mapValueOfType<String>(json, r'ui_type')!,
        categoryCommaOmitempty: mapValueOfType<String>(json, r'category,omitempty'),
        tagsCommaOmitempty: mapValueOfType<String>(json, r'tags,omitempty'),
        examplesCommaOmitempty: mapValueOfType<String>(json, r'examples,omitempty'),
        riskLevelCommaOmitempty: mapValueOfType<String>(json, r'risk_level,omitempty'),
        bodyCommaOmitempty: mapValueOfType<String>(json, r'body,omitempty'),
        fieldsCommaOmitempty: json[r'fields,omitempty'] is Iterable
            ? (json[r'fields,omitempty'] as Iterable).cast<String>().toList(growable: false)
            : const [],
        actionsCommaOmitempty: json[r'actions,omitempty'] is Iterable
            ? (json[r'actions,omitempty'] as Iterable).cast<String>().toList(growable: false)
            : const [],
      );
    }
    return null;
  }

  static List<SkillUIDescriptor> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SkillUIDescriptor>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SkillUIDescriptor.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SkillUIDescriptor> mapFromJson(dynamic json) {
    final map = <String, SkillUIDescriptor>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SkillUIDescriptor.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SkillUIDescriptor-objects as value to a dart map
  static Map<String, List<SkillUIDescriptor>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SkillUIDescriptor>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SkillUIDescriptor.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'slug',
    'name',
    'description',
    'ui_type',
  };
}

