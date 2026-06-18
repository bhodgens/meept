// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'bus_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$BusService extends BusService {
  @override
  final JsonObject? bus;

  factory _$BusService([void Function(BusServiceBuilder)? updates]) =>
      (BusServiceBuilder()..update(updates))._build();

  _$BusService._({this.bus}) : super._();
  @override
  BusService rebuild(void Function(BusServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  BusServiceBuilder toBuilder() => BusServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is BusService && bus == other.bus;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, bus.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'BusService')..add('bus', bus))
        .toString();
  }
}

class BusServiceBuilder implements Builder<BusService, BusServiceBuilder> {
  _$BusService? _$v;

  JsonObject? _bus;
  JsonObject? get bus => _$this._bus;
  set bus(JsonObject? bus) => _$this._bus = bus;

  BusServiceBuilder() {
    BusService._defaults(this);
  }

  BusServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _bus = $v.bus;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(BusService other) {
    _$v = other as _$BusService;
  }

  @override
  void update(void Function(BusServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  BusService build() => _build();

  _$BusService _build() {
    final _$result = _$v ??
        _$BusService._(
          bus: bus,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
