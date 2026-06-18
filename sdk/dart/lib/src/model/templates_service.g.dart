// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'templates_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TemplatesService extends TemplatesService {
  @override
  final JsonObject? registry;
  @override
  final JsonObject? executor;

  factory _$TemplatesService(
          [void Function(TemplatesServiceBuilder)? updates]) =>
      (TemplatesServiceBuilder()..update(updates))._build();

  _$TemplatesService._({this.registry, this.executor}) : super._();
  @override
  TemplatesService rebuild(void Function(TemplatesServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TemplatesServiceBuilder toBuilder() =>
      TemplatesServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TemplatesService &&
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
    return (newBuiltValueToStringHelper(r'TemplatesService')
          ..add('registry', registry)
          ..add('executor', executor))
        .toString();
  }
}

class TemplatesServiceBuilder
    implements Builder<TemplatesService, TemplatesServiceBuilder> {
  _$TemplatesService? _$v;

  JsonObject? _registry;
  JsonObject? get registry => _$this._registry;
  set registry(JsonObject? registry) => _$this._registry = registry;

  JsonObject? _executor;
  JsonObject? get executor => _$this._executor;
  set executor(JsonObject? executor) => _$this._executor = executor;

  TemplatesServiceBuilder() {
    TemplatesService._defaults(this);
  }

  TemplatesServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _registry = $v.registry;
      _executor = $v.executor;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TemplatesService other) {
    _$v = other as _$TemplatesService;
  }

  @override
  void update(void Function(TemplatesServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TemplatesService build() => _build();

  _$TemplatesService _build() {
    final _$result = _$v ??
        _$TemplatesService._(
          registry: registry,
          executor: executor,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
