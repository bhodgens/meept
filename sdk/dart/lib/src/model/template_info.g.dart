// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'template_info.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TemplateInfo extends TemplateInfo {
  @override
  final String name;
  @override
  final String description;
  @override
  final JsonObject scope;
  @override
  final String? pathCommaOmitempty;
  @override
  final int priority;
  @override
  final String? bodyCommaOmitempty;

  factory _$TemplateInfo([void Function(TemplateInfoBuilder)? updates]) =>
      (TemplateInfoBuilder()..update(updates))._build();

  _$TemplateInfo._(
      {required this.name,
      required this.description,
      required this.scope,
      this.pathCommaOmitempty,
      required this.priority,
      this.bodyCommaOmitempty})
      : super._();
  @override
  TemplateInfo rebuild(void Function(TemplateInfoBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TemplateInfoBuilder toBuilder() => TemplateInfoBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TemplateInfo &&
        name == other.name &&
        description == other.description &&
        scope == other.scope &&
        pathCommaOmitempty == other.pathCommaOmitempty &&
        priority == other.priority &&
        bodyCommaOmitempty == other.bodyCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, description.hashCode);
    _$hash = $jc(_$hash, scope.hashCode);
    _$hash = $jc(_$hash, pathCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, priority.hashCode);
    _$hash = $jc(_$hash, bodyCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TemplateInfo')
          ..add('name', name)
          ..add('description', description)
          ..add('scope', scope)
          ..add('pathCommaOmitempty', pathCommaOmitempty)
          ..add('priority', priority)
          ..add('bodyCommaOmitempty', bodyCommaOmitempty))
        .toString();
  }
}

class TemplateInfoBuilder
    implements Builder<TemplateInfo, TemplateInfoBuilder> {
  _$TemplateInfo? _$v;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _description;
  String? get description => _$this._description;
  set description(String? description) => _$this._description = description;

  JsonObject? _scope;
  JsonObject? get scope => _$this._scope;
  set scope(JsonObject? scope) => _$this._scope = scope;

  String? _pathCommaOmitempty;
  String? get pathCommaOmitempty => _$this._pathCommaOmitempty;
  set pathCommaOmitempty(String? pathCommaOmitempty) =>
      _$this._pathCommaOmitempty = pathCommaOmitempty;

  int? _priority;
  int? get priority => _$this._priority;
  set priority(int? priority) => _$this._priority = priority;

  String? _bodyCommaOmitempty;
  String? get bodyCommaOmitempty => _$this._bodyCommaOmitempty;
  set bodyCommaOmitempty(String? bodyCommaOmitempty) =>
      _$this._bodyCommaOmitempty = bodyCommaOmitempty;

  TemplateInfoBuilder() {
    TemplateInfo._defaults(this);
  }

  TemplateInfoBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _name = $v.name;
      _description = $v.description;
      _scope = $v.scope;
      _pathCommaOmitempty = $v.pathCommaOmitempty;
      _priority = $v.priority;
      _bodyCommaOmitempty = $v.bodyCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TemplateInfo other) {
    _$v = other as _$TemplateInfo;
  }

  @override
  void update(void Function(TemplateInfoBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TemplateInfo build() => _build();

  _$TemplateInfo _build() {
    final _$result = _$v ??
        _$TemplateInfo._(
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'TemplateInfo', 'name'),
          description: BuiltValueNullFieldError.checkNotNull(
              description, r'TemplateInfo', 'description'),
          scope: BuiltValueNullFieldError.checkNotNull(
              scope, r'TemplateInfo', 'scope'),
          pathCommaOmitempty: pathCommaOmitempty,
          priority: BuiltValueNullFieldError.checkNotNull(
              priority, r'TemplateInfo', 'priority'),
          bodyCommaOmitempty: bodyCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
