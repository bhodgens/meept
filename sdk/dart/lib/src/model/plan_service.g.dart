// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'plan_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$PlanService extends PlanService {
  @override
  final JsonObject? manager;
  @override
  final JsonObject? store;

  factory _$PlanService([void Function(PlanServiceBuilder)? updates]) =>
      (PlanServiceBuilder()..update(updates))._build();

  _$PlanService._({this.manager, this.store}) : super._();
  @override
  PlanService rebuild(void Function(PlanServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  PlanServiceBuilder toBuilder() => PlanServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is PlanService &&
        manager == other.manager &&
        store == other.store;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, manager.hashCode);
    _$hash = $jc(_$hash, store.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'PlanService')
          ..add('manager', manager)
          ..add('store', store))
        .toString();
  }
}

class PlanServiceBuilder implements Builder<PlanService, PlanServiceBuilder> {
  _$PlanService? _$v;

  JsonObject? _manager;
  JsonObject? get manager => _$this._manager;
  set manager(JsonObject? manager) => _$this._manager = manager;

  JsonObject? _store;
  JsonObject? get store => _$this._store;
  set store(JsonObject? store) => _$this._store = store;

  PlanServiceBuilder() {
    PlanService._defaults(this);
  }

  PlanServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _manager = $v.manager;
      _store = $v.store;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(PlanService other) {
    _$v = other as _$PlanService;
  }

  @override
  void update(void Function(PlanServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  PlanService build() => _build();

  _$PlanService _build() {
    final _$result = _$v ??
        _$PlanService._(
          manager: manager,
          store: store,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
