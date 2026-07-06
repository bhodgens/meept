// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'audit_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$AuditRequest extends AuditRequest {
  @override
  final int? limit;

  factory _$AuditRequest([void Function(AuditRequestBuilder)? updates]) =>
      (AuditRequestBuilder()..update(updates))._build();

  _$AuditRequest._({this.limit}) : super._();
  @override
  AuditRequest rebuild(void Function(AuditRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  AuditRequestBuilder toBuilder() => AuditRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is AuditRequest &&
        limit == other.limit;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, limit.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'AuditRequest')
          ..add('limit', limit))
        .toString();
  }
}

class AuditRequestBuilder
    implements Builder<AuditRequest, AuditRequestBuilder> {
  _$AuditRequest? _$v;

  int? _limit;
  int? get limit => _$this._limit;
  set limit(int? limit) =>
      _$this._limit = limit;

  AuditRequestBuilder() {
    AuditRequest._defaults(this);
  }

  AuditRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _limit = $v.limit;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(AuditRequest other) {
    _$v = other as _$AuditRequest;
  }

  @override
  void update(void Function(AuditRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  AuditRequest build() => _build();

  _$AuditRequest _build() {
    final _$result = _$v ??
        _$AuditRequest._(
          limit: limit,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
