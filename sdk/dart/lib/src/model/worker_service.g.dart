// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'worker_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$WorkerService extends WorkerService {
  @override
  final JsonObject? pool;

  factory _$WorkerService([void Function(WorkerServiceBuilder)? updates]) =>
      (WorkerServiceBuilder()..update(updates))._build();

  _$WorkerService._({this.pool}) : super._();
  @override
  WorkerService rebuild(void Function(WorkerServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  WorkerServiceBuilder toBuilder() => WorkerServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is WorkerService && pool == other.pool;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, pool.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'WorkerService')..add('pool', pool))
        .toString();
  }
}

class WorkerServiceBuilder
    implements Builder<WorkerService, WorkerServiceBuilder> {
  _$WorkerService? _$v;

  JsonObject? _pool;
  JsonObject? get pool => _$this._pool;
  set pool(JsonObject? pool) => _$this._pool = pool;

  WorkerServiceBuilder() {
    WorkerService._defaults(this);
  }

  WorkerServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _pool = $v.pool;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(WorkerService other) {
    _$v = other as _$WorkerService;
  }

  @override
  void update(void Function(WorkerServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  WorkerService build() => _build();

  _$WorkerService _build() {
    final _$result = _$v ??
        _$WorkerService._(
          pool: pool,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
