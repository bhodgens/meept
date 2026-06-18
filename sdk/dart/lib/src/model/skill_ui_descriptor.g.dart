// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'skill_ui_descriptor.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SkillUIDescriptor extends SkillUIDescriptor {
  @override
  final String slug;
  @override
  final String name;
  @override
  final String description;
  @override
  final String uiType;
  @override
  final String? categoryCommaOmitempty;
  @override
  final String? tagsCommaOmitempty;
  @override
  final String? examplesCommaOmitempty;
  @override
  final String? riskLevelCommaOmitempty;
  @override
  final String? bodyCommaOmitempty;
  @override
  final BuiltList<String>? fieldsCommaOmitempty;
  @override
  final BuiltList<String>? actionsCommaOmitempty;

  factory _$SkillUIDescriptor(
          [void Function(SkillUIDescriptorBuilder)? updates]) =>
      (SkillUIDescriptorBuilder()..update(updates))._build();

  _$SkillUIDescriptor._(
      {required this.slug,
      required this.name,
      required this.description,
      required this.uiType,
      this.categoryCommaOmitempty,
      this.tagsCommaOmitempty,
      this.examplesCommaOmitempty,
      this.riskLevelCommaOmitempty,
      this.bodyCommaOmitempty,
      this.fieldsCommaOmitempty,
      this.actionsCommaOmitempty})
      : super._();
  @override
  SkillUIDescriptor rebuild(void Function(SkillUIDescriptorBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SkillUIDescriptorBuilder toBuilder() =>
      SkillUIDescriptorBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SkillUIDescriptor &&
        slug == other.slug &&
        name == other.name &&
        description == other.description &&
        uiType == other.uiType &&
        categoryCommaOmitempty == other.categoryCommaOmitempty &&
        tagsCommaOmitempty == other.tagsCommaOmitempty &&
        examplesCommaOmitempty == other.examplesCommaOmitempty &&
        riskLevelCommaOmitempty == other.riskLevelCommaOmitempty &&
        bodyCommaOmitempty == other.bodyCommaOmitempty &&
        fieldsCommaOmitempty == other.fieldsCommaOmitempty &&
        actionsCommaOmitempty == other.actionsCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, slug.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, description.hashCode);
    _$hash = $jc(_$hash, uiType.hashCode);
    _$hash = $jc(_$hash, categoryCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, tagsCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, examplesCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, riskLevelCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, bodyCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, fieldsCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, actionsCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SkillUIDescriptor')
          ..add('slug', slug)
          ..add('name', name)
          ..add('description', description)
          ..add('uiType', uiType)
          ..add('categoryCommaOmitempty', categoryCommaOmitempty)
          ..add('tagsCommaOmitempty', tagsCommaOmitempty)
          ..add('examplesCommaOmitempty', examplesCommaOmitempty)
          ..add('riskLevelCommaOmitempty', riskLevelCommaOmitempty)
          ..add('bodyCommaOmitempty', bodyCommaOmitempty)
          ..add('fieldsCommaOmitempty', fieldsCommaOmitempty)
          ..add('actionsCommaOmitempty', actionsCommaOmitempty))
        .toString();
  }
}

class SkillUIDescriptorBuilder
    implements Builder<SkillUIDescriptor, SkillUIDescriptorBuilder> {
  _$SkillUIDescriptor? _$v;

  String? _slug;
  String? get slug => _$this._slug;
  set slug(String? slug) => _$this._slug = slug;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _description;
  String? get description => _$this._description;
  set description(String? description) => _$this._description = description;

  String? _uiType;
  String? get uiType => _$this._uiType;
  set uiType(String? uiType) => _$this._uiType = uiType;

  String? _categoryCommaOmitempty;
  String? get categoryCommaOmitempty => _$this._categoryCommaOmitempty;
  set categoryCommaOmitempty(String? categoryCommaOmitempty) =>
      _$this._categoryCommaOmitempty = categoryCommaOmitempty;

  String? _tagsCommaOmitempty;
  String? get tagsCommaOmitempty => _$this._tagsCommaOmitempty;
  set tagsCommaOmitempty(String? tagsCommaOmitempty) =>
      _$this._tagsCommaOmitempty = tagsCommaOmitempty;

  String? _examplesCommaOmitempty;
  String? get examplesCommaOmitempty => _$this._examplesCommaOmitempty;
  set examplesCommaOmitempty(String? examplesCommaOmitempty) =>
      _$this._examplesCommaOmitempty = examplesCommaOmitempty;

  String? _riskLevelCommaOmitempty;
  String? get riskLevelCommaOmitempty => _$this._riskLevelCommaOmitempty;
  set riskLevelCommaOmitempty(String? riskLevelCommaOmitempty) =>
      _$this._riskLevelCommaOmitempty = riskLevelCommaOmitempty;

  String? _bodyCommaOmitempty;
  String? get bodyCommaOmitempty => _$this._bodyCommaOmitempty;
  set bodyCommaOmitempty(String? bodyCommaOmitempty) =>
      _$this._bodyCommaOmitempty = bodyCommaOmitempty;

  ListBuilder<String>? _fieldsCommaOmitempty;
  ListBuilder<String> get fieldsCommaOmitempty =>
      _$this._fieldsCommaOmitempty ??= ListBuilder<String>();
  set fieldsCommaOmitempty(ListBuilder<String>? fieldsCommaOmitempty) =>
      _$this._fieldsCommaOmitempty = fieldsCommaOmitempty;

  ListBuilder<String>? _actionsCommaOmitempty;
  ListBuilder<String> get actionsCommaOmitempty =>
      _$this._actionsCommaOmitempty ??= ListBuilder<String>();
  set actionsCommaOmitempty(ListBuilder<String>? actionsCommaOmitempty) =>
      _$this._actionsCommaOmitempty = actionsCommaOmitempty;

  SkillUIDescriptorBuilder() {
    SkillUIDescriptor._defaults(this);
  }

  SkillUIDescriptorBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _slug = $v.slug;
      _name = $v.name;
      _description = $v.description;
      _uiType = $v.uiType;
      _categoryCommaOmitempty = $v.categoryCommaOmitempty;
      _tagsCommaOmitempty = $v.tagsCommaOmitempty;
      _examplesCommaOmitempty = $v.examplesCommaOmitempty;
      _riskLevelCommaOmitempty = $v.riskLevelCommaOmitempty;
      _bodyCommaOmitempty = $v.bodyCommaOmitempty;
      _fieldsCommaOmitempty = $v.fieldsCommaOmitempty?.toBuilder();
      _actionsCommaOmitempty = $v.actionsCommaOmitempty?.toBuilder();
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SkillUIDescriptor other) {
    _$v = other as _$SkillUIDescriptor;
  }

  @override
  void update(void Function(SkillUIDescriptorBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SkillUIDescriptor build() => _build();

  _$SkillUIDescriptor _build() {
    _$SkillUIDescriptor _$result;
    try {
      _$result = _$v ??
          _$SkillUIDescriptor._(
            slug: BuiltValueNullFieldError.checkNotNull(
                slug, r'SkillUIDescriptor', 'slug'),
            name: BuiltValueNullFieldError.checkNotNull(
                name, r'SkillUIDescriptor', 'name'),
            description: BuiltValueNullFieldError.checkNotNull(
                description, r'SkillUIDescriptor', 'description'),
            uiType: BuiltValueNullFieldError.checkNotNull(
                uiType, r'SkillUIDescriptor', 'uiType'),
            categoryCommaOmitempty: categoryCommaOmitempty,
            tagsCommaOmitempty: tagsCommaOmitempty,
            examplesCommaOmitempty: examplesCommaOmitempty,
            riskLevelCommaOmitempty: riskLevelCommaOmitempty,
            bodyCommaOmitempty: bodyCommaOmitempty,
            fieldsCommaOmitempty: _fieldsCommaOmitempty?.build(),
            actionsCommaOmitempty: _actionsCommaOmitempty?.build(),
          );
    } catch (_) {
      late String _$failedField;
      try {
        _$failedField = 'fieldsCommaOmitempty';
        _fieldsCommaOmitempty?.build();
        _$failedField = 'actionsCommaOmitempty';
        _actionsCommaOmitempty?.build();
      } catch (e) {
        throw BuiltValueNestedFieldError(
            r'SkillUIDescriptor', _$failedField, e.toString());
      }
      rethrow;
    }
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
