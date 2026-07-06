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
  final String? category;
  @override
  final String? tags;
  @override
  final String? examples;
  @override
  final String? riskLevel;
  @override
  final String? body;
  @override
  final BuiltList<String>? fields;
  @override
  final BuiltList<String>? actions;

  factory _$SkillUIDescriptor(
          [void Function(SkillUIDescriptorBuilder)? updates]) =>
      (SkillUIDescriptorBuilder()..update(updates))._build();

  _$SkillUIDescriptor._(
      {required this.slug,
      required this.name,
      required this.description,
      required this.uiType,
      this.category,
      this.tags,
      this.examples,
      this.riskLevel,
      this.body,
      this.fields,
      this.actions})
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
        category == other.category &&
        tags == other.tags &&
        examples == other.examples &&
        riskLevel == other.riskLevel &&
        body == other.body &&
        fields == other.fields &&
        actions == other.actions;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, slug.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, description.hashCode);
    _$hash = $jc(_$hash, uiType.hashCode);
    _$hash = $jc(_$hash, category.hashCode);
    _$hash = $jc(_$hash, tags.hashCode);
    _$hash = $jc(_$hash, examples.hashCode);
    _$hash = $jc(_$hash, riskLevel.hashCode);
    _$hash = $jc(_$hash, body.hashCode);
    _$hash = $jc(_$hash, fields.hashCode);
    _$hash = $jc(_$hash, actions.hashCode);
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
          ..add('category', category)
          ..add('tags', tags)
          ..add('examples', examples)
          ..add('riskLevel', riskLevel)
          ..add('body', body)
          ..add('fields', fields)
          ..add('actions', actions))
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

  String? _category;
  String? get category => _$this._category;
  set category(String? category) =>
      _$this._category = category;

  String? _tags;
  String? get tags => _$this._tags;
  set tags(String? tags) =>
      _$this._tags = tags;

  String? _examples;
  String? get examples => _$this._examples;
  set examples(String? examples) =>
      _$this._examples = examples;

  String? _riskLevel;
  String? get riskLevel => _$this._riskLevel;
  set riskLevel(String? riskLevel) =>
      _$this._riskLevel = riskLevel;

  String? _body;
  String? get body => _$this._body;
  set body(String? body) =>
      _$this._body = body;

  ListBuilder<String>? _fields;
  ListBuilder<String> get fields =>
      _$this._fields ??= ListBuilder<String>();
  set fields(ListBuilder<String>? fields) =>
      _$this._fields = fields;

  ListBuilder<String>? _actions;
  ListBuilder<String> get actions =>
      _$this._actions ??= ListBuilder<String>();
  set actions(ListBuilder<String>? actions) =>
      _$this._actions = actions;

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
      _category = $v.category;
      _tags = $v.tags;
      _examples = $v.examples;
      _riskLevel = $v.riskLevel;
      _body = $v.body;
      _fields = $v.fields?.toBuilder();
      _actions = $v.actions?.toBuilder();
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
            category: category,
            tags: tags,
            examples: examples,
            riskLevel: riskLevel,
            body: body,
            fields: _fields?.build(),
            actions: _actions?.build(),
          );
    } catch (_) {
      late String _$failedField;
      try {
        _$failedField = 'fields';
        _fields?.build();
        _$failedField = 'actions';
        _actions?.build();
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
