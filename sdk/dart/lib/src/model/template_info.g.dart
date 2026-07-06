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
  final String? path;
  @override
  final int priority;
  @override
  final String? body;

  factory _$TemplateInfo([void Function(TemplateInfoBuilder)? updates]) =>
      (TemplateInfoBuilder()..update(updates))._build();

  _$TemplateInfo._(
      {required this.name,
      required this.description,
      required this.scope,
      this.path,
      required this.priority,
      this.body})
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
        path == other.path &&
        priority == other.priority &&
        body == other.body;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, description.hashCode);
    _$hash = $jc(_$hash, scope.hashCode);
    _$hash = $jc(_$hash, path.hashCode);
    _$hash = $jc(_$hash, priority.hashCode);
    _$hash = $jc(_$hash, body.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TemplateInfo')
          ..add('name', name)
          ..add('description', description)
          ..add('scope', scope)
          ..add('path', path)
          ..add('priority', priority)
          ..add('body', body))
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

  String? _path;
  String? get path => _$this._path;
  set path(String? path) =>
      _$this._path = path;

  int? _priority;
  int? get priority => _$this._priority;
  set priority(int? priority) => _$this._priority = priority;

  String? _body;
  String? get body => _$this._body;
  set body(String? body) =>
      _$this._body = body;

  TemplateInfoBuilder() {
    TemplateInfo._defaults(this);
  }

  TemplateInfoBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _name = $v.name;
      _description = $v.description;
      _scope = $v.scope;
      _path = $v.path;
      _priority = $v.priority;
      _body = $v.body;
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
          path: path,
          priority: BuiltValueNullFieldError.checkNotNull(
              priority, r'TemplateInfo', 'priority'),
          body: body,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
