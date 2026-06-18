// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'runtime_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$RuntimeService extends RuntimeService {
  @override
  final JsonObject? manager;

  factory _$RuntimeService([void Function(RuntimeServiceBuilder)? updates]) =>
      (RuntimeServiceBuilder()..update(updates))._build();

  _$RuntimeService._({this.manager}) : super._();
  @override
  RuntimeService rebuild(void Function(RuntimeServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  RuntimeServiceBuilder toBuilder() => RuntimeServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is RuntimeService && manager == other.manager;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, manager.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'RuntimeService')
          ..add('manager', manager))
        .toString();
  }
}

class RuntimeServiceBuilder
    implements Builder<RuntimeService, RuntimeServiceBuilder> {
  _$RuntimeService? _$v;

  JsonObject? _manager;
  JsonObject? get manager => _$this._manager;
  set manager(JsonObject? manager) => _$this._manager = manager;

  RuntimeServiceBuilder() {
    RuntimeService._defaults(this);
  }

  RuntimeServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _manager = $v.manager;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(RuntimeService other) {
    _$v = other as _$RuntimeService;
  }

  @override
  void update(void Function(RuntimeServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  RuntimeService build() => _build();

  _$RuntimeService _build() {
    final _$result = _$v ??
        _$RuntimeService._(
          manager: manager,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
