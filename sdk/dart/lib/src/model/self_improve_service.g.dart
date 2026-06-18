// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'self_improve_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SelfImproveService extends SelfImproveService {
  @override
  final JsonObject? controller;

  factory _$SelfImproveService(
          [void Function(SelfImproveServiceBuilder)? updates]) =>
      (SelfImproveServiceBuilder()..update(updates))._build();

  _$SelfImproveService._({this.controller}) : super._();
  @override
  SelfImproveService rebuild(
          void Function(SelfImproveServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SelfImproveServiceBuilder toBuilder() =>
      SelfImproveServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SelfImproveService && controller == other.controller;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, controller.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SelfImproveService')
          ..add('controller', controller))
        .toString();
  }
}

class SelfImproveServiceBuilder
    implements Builder<SelfImproveService, SelfImproveServiceBuilder> {
  _$SelfImproveService? _$v;

  JsonObject? _controller;
  JsonObject? get controller => _$this._controller;
  set controller(JsonObject? controller) => _$this._controller = controller;

  SelfImproveServiceBuilder() {
    SelfImproveService._defaults(this);
  }

  SelfImproveServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _controller = $v.controller;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SelfImproveService other) {
    _$v = other as _$SelfImproveService;
  }

  @override
  void update(void Function(SelfImproveServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SelfImproveService build() => _build();

  _$SelfImproveService _build() {
    final _$result = _$v ??
        _$SelfImproveService._(
          controller: controller,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
