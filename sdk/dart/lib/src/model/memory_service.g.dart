// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'memory_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$MemoryService extends MemoryService {
  @override
  final JsonObject? manager;

  factory _$MemoryService([void Function(MemoryServiceBuilder)? updates]) =>
      (MemoryServiceBuilder()..update(updates))._build();

  _$MemoryService._({this.manager}) : super._();
  @override
  MemoryService rebuild(void Function(MemoryServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  MemoryServiceBuilder toBuilder() => MemoryServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is MemoryService && manager == other.manager;
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
    return (newBuiltValueToStringHelper(r'MemoryService')
          ..add('manager', manager))
        .toString();
  }
}

class MemoryServiceBuilder
    implements Builder<MemoryService, MemoryServiceBuilder> {
  _$MemoryService? _$v;

  JsonObject? _manager;
  JsonObject? get manager => _$this._manager;
  set manager(JsonObject? manager) => _$this._manager = manager;

  MemoryServiceBuilder() {
    MemoryService._defaults(this);
  }

  MemoryServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _manager = $v.manager;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(MemoryService other) {
    _$v = other as _$MemoryService;
  }

  @override
  void update(void Function(MemoryServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  MemoryService build() => _build();

  _$MemoryService _build() {
    final _$result = _$v ??
        _$MemoryService._(
          manager: manager,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
