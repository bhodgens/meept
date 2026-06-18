// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'skills_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SkillsService extends SkillsService {
  @override
  final JsonObject? registry;
  @override
  final JsonObject? executor;

  factory _$SkillsService([void Function(SkillsServiceBuilder)? updates]) =>
      (SkillsServiceBuilder()..update(updates))._build();

  _$SkillsService._({this.registry, this.executor}) : super._();
  @override
  SkillsService rebuild(void Function(SkillsServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SkillsServiceBuilder toBuilder() => SkillsServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SkillsService &&
        registry == other.registry &&
        executor == other.executor;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, registry.hashCode);
    _$hash = $jc(_$hash, executor.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SkillsService')
          ..add('registry', registry)
          ..add('executor', executor))
        .toString();
  }
}

class SkillsServiceBuilder
    implements Builder<SkillsService, SkillsServiceBuilder> {
  _$SkillsService? _$v;

  JsonObject? _registry;
  JsonObject? get registry => _$this._registry;
  set registry(JsonObject? registry) => _$this._registry = registry;

  JsonObject? _executor;
  JsonObject? get executor => _$this._executor;
  set executor(JsonObject? executor) => _$this._executor = executor;

  SkillsServiceBuilder() {
    SkillsService._defaults(this);
  }

  SkillsServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _registry = $v.registry;
      _executor = $v.executor;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SkillsService other) {
    _$v = other as _$SkillsService;
  }

  @override
  void update(void Function(SkillsServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SkillsService build() => _build();

  _$SkillsService _build() {
    final _$result = _$v ??
        _$SkillsService._(
          registry: registry,
          executor: executor,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
