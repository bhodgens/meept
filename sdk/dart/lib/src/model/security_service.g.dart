// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'security_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SecurityService extends SecurityService {
  @override
  final JsonObject? checker;

  factory _$SecurityService([void Function(SecurityServiceBuilder)? updates]) =>
      (SecurityServiceBuilder()..update(updates))._build();

  _$SecurityService._({this.checker}) : super._();
  @override
  SecurityService rebuild(void Function(SecurityServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SecurityServiceBuilder toBuilder() => SecurityServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SecurityService && checker == other.checker;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, checker.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SecurityService')
          ..add('checker', checker))
        .toString();
  }
}

class SecurityServiceBuilder
    implements Builder<SecurityService, SecurityServiceBuilder> {
  _$SecurityService? _$v;

  JsonObject? _checker;
  JsonObject? get checker => _$this._checker;
  set checker(JsonObject? checker) => _$this._checker = checker;

  SecurityServiceBuilder() {
    SecurityService._defaults(this);
  }

  SecurityServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _checker = $v.checker;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SecurityService other) {
    _$v = other as _$SecurityService;
  }

  @override
  void update(void Function(SecurityServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SecurityService build() => _build();

  _$SecurityService _build() {
    final _$result = _$v ??
        _$SecurityService._(
          checker: checker,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
