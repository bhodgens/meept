// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'check_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CheckResponse extends CheckResponse {
  @override
  final bool allowed;
  @override
  final String? reason;

  factory _$CheckResponse([void Function(CheckResponseBuilder)? updates]) =>
      (CheckResponseBuilder()..update(updates))._build();

  _$CheckResponse._({required this.allowed, this.reason})
      : super._();
  @override
  CheckResponse rebuild(void Function(CheckResponseBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CheckResponseBuilder toBuilder() => CheckResponseBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CheckResponse &&
        allowed == other.allowed &&
        reason == other.reason;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, allowed.hashCode);
    _$hash = $jc(_$hash, reason.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CheckResponse')
          ..add('allowed', allowed)
          ..add('reason', reason))
        .toString();
  }
}

class CheckResponseBuilder
    implements Builder<CheckResponse, CheckResponseBuilder> {
  _$CheckResponse? _$v;

  bool? _allowed;
  bool? get allowed => _$this._allowed;
  set allowed(bool? allowed) => _$this._allowed = allowed;

  String? _reason;
  String? get reason => _$this._reason;
  set reason(String? reason) =>
      _$this._reason = reason;

  CheckResponseBuilder() {
    CheckResponse._defaults(this);
  }

  CheckResponseBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _allowed = $v.allowed;
      _reason = $v.reason;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CheckResponse other) {
    _$v = other as _$CheckResponse;
  }

  @override
  void update(void Function(CheckResponseBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CheckResponse build() => _build();

  _$CheckResponse _build() {
    final _$result = _$v ??
        _$CheckResponse._(
          allowed: BuiltValueNullFieldError.checkNotNull(
              allowed, r'CheckResponse', 'allowed'),
          reason: reason,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
