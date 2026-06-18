// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'skill_info.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SkillInfo extends SkillInfo {
  @override
  final String slug;
  @override
  final String name;
  @override
  final String description;
  @override
  final String? categoryCommaOmitempty;
  @override
  final String? capabilitiesCommaOmitempty;
  @override
  final bool enabled;
  @override
  final String? uiTypeCommaOmitempty;

  factory _$SkillInfo([void Function(SkillInfoBuilder)? updates]) =>
      (SkillInfoBuilder()..update(updates))._build();

  _$SkillInfo._(
      {required this.slug,
      required this.name,
      required this.description,
      this.categoryCommaOmitempty,
      this.capabilitiesCommaOmitempty,
      required this.enabled,
      this.uiTypeCommaOmitempty})
      : super._();
  @override
  SkillInfo rebuild(void Function(SkillInfoBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SkillInfoBuilder toBuilder() => SkillInfoBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SkillInfo &&
        slug == other.slug &&
        name == other.name &&
        description == other.description &&
        categoryCommaOmitempty == other.categoryCommaOmitempty &&
        capabilitiesCommaOmitempty == other.capabilitiesCommaOmitempty &&
        enabled == other.enabled &&
        uiTypeCommaOmitempty == other.uiTypeCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, slug.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, description.hashCode);
    _$hash = $jc(_$hash, categoryCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, capabilitiesCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, enabled.hashCode);
    _$hash = $jc(_$hash, uiTypeCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SkillInfo')
          ..add('slug', slug)
          ..add('name', name)
          ..add('description', description)
          ..add('categoryCommaOmitempty', categoryCommaOmitempty)
          ..add('capabilitiesCommaOmitempty', capabilitiesCommaOmitempty)
          ..add('enabled', enabled)
          ..add('uiTypeCommaOmitempty', uiTypeCommaOmitempty))
        .toString();
  }
}

class SkillInfoBuilder implements Builder<SkillInfo, SkillInfoBuilder> {
  _$SkillInfo? _$v;

  String? _slug;
  String? get slug => _$this._slug;
  set slug(String? slug) => _$this._slug = slug;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _description;
  String? get description => _$this._description;
  set description(String? description) => _$this._description = description;

  String? _categoryCommaOmitempty;
  String? get categoryCommaOmitempty => _$this._categoryCommaOmitempty;
  set categoryCommaOmitempty(String? categoryCommaOmitempty) =>
      _$this._categoryCommaOmitempty = categoryCommaOmitempty;

  String? _capabilitiesCommaOmitempty;
  String? get capabilitiesCommaOmitempty => _$this._capabilitiesCommaOmitempty;
  set capabilitiesCommaOmitempty(String? capabilitiesCommaOmitempty) =>
      _$this._capabilitiesCommaOmitempty = capabilitiesCommaOmitempty;

  bool? _enabled;
  bool? get enabled => _$this._enabled;
  set enabled(bool? enabled) => _$this._enabled = enabled;

  String? _uiTypeCommaOmitempty;
  String? get uiTypeCommaOmitempty => _$this._uiTypeCommaOmitempty;
  set uiTypeCommaOmitempty(String? uiTypeCommaOmitempty) =>
      _$this._uiTypeCommaOmitempty = uiTypeCommaOmitempty;

  SkillInfoBuilder() {
    SkillInfo._defaults(this);
  }

  SkillInfoBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _slug = $v.slug;
      _name = $v.name;
      _description = $v.description;
      _categoryCommaOmitempty = $v.categoryCommaOmitempty;
      _capabilitiesCommaOmitempty = $v.capabilitiesCommaOmitempty;
      _enabled = $v.enabled;
      _uiTypeCommaOmitempty = $v.uiTypeCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SkillInfo other) {
    _$v = other as _$SkillInfo;
  }

  @override
  void update(void Function(SkillInfoBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SkillInfo build() => _build();

  _$SkillInfo _build() {
    final _$result = _$v ??
        _$SkillInfo._(
          slug:
              BuiltValueNullFieldError.checkNotNull(slug, r'SkillInfo', 'slug'),
          name:
              BuiltValueNullFieldError.checkNotNull(name, r'SkillInfo', 'name'),
          description: BuiltValueNullFieldError.checkNotNull(
              description, r'SkillInfo', 'description'),
          categoryCommaOmitempty: categoryCommaOmitempty,
          capabilitiesCommaOmitempty: capabilitiesCommaOmitempty,
          enabled: BuiltValueNullFieldError.checkNotNull(
              enabled, r'SkillInfo', 'enabled'),
          uiTypeCommaOmitempty: uiTypeCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
