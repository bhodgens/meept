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
  final String? category;
  @override
  final String? capabilities;
  @override
  final bool enabled;
  @override
  final String? uiType;

  factory _$SkillInfo([void Function(SkillInfoBuilder)? updates]) =>
      (SkillInfoBuilder()..update(updates))._build();

  _$SkillInfo._(
      {required this.slug,
      required this.name,
      required this.description,
      this.category,
      this.capabilities,
      required this.enabled,
      this.uiType})
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
        category == other.category &&
        capabilities == other.capabilities &&
        enabled == other.enabled &&
        uiType == other.uiType;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, slug.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, description.hashCode);
    _$hash = $jc(_$hash, category.hashCode);
    _$hash = $jc(_$hash, capabilities.hashCode);
    _$hash = $jc(_$hash, enabled.hashCode);
    _$hash = $jc(_$hash, uiType.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SkillInfo')
          ..add('slug', slug)
          ..add('name', name)
          ..add('description', description)
          ..add('category', category)
          ..add('capabilities', capabilities)
          ..add('enabled', enabled)
          ..add('uiType', uiType))
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

  String? _category;
  String? get category => _$this._category;
  set category(String? category) =>
      _$this._category = category;

  String? _capabilities;
  String? get capabilities => _$this._capabilities;
  set capabilities(String? capabilities) =>
      _$this._capabilities = capabilities;

  bool? _enabled;
  bool? get enabled => _$this._enabled;
  set enabled(bool? enabled) => _$this._enabled = enabled;

  String? _uiType;
  String? get uiType => _$this._uiType;
  set uiType(String? uiType) =>
      _$this._uiType = uiType;

  SkillInfoBuilder() {
    SkillInfo._defaults(this);
  }

  SkillInfoBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _slug = $v.slug;
      _name = $v.name;
      _description = $v.description;
      _category = $v.category;
      _capabilities = $v.capabilities;
      _enabled = $v.enabled;
      _uiType = $v.uiType;
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
          category: category,
          capabilities: capabilities,
          enabled: BuiltValueNullFieldError.checkNotNull(
              enabled, r'SkillInfo', 'enabled'),
          uiType: uiType,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
