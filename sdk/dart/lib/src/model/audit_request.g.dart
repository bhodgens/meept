// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'audit_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$AuditRequest extends AuditRequest {
  @override
  final int? limitCommaOmitempty;

  factory _$AuditRequest([void Function(AuditRequestBuilder)? updates]) =>
      (AuditRequestBuilder()..update(updates))._build();

  _$AuditRequest._({this.limitCommaOmitempty}) : super._();
  @override
  AuditRequest rebuild(void Function(AuditRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  AuditRequestBuilder toBuilder() => AuditRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is AuditRequest &&
        limitCommaOmitempty == other.limitCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, limitCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'AuditRequest')
          ..add('limitCommaOmitempty', limitCommaOmitempty))
        .toString();
  }
}

class AuditRequestBuilder
    implements Builder<AuditRequest, AuditRequestBuilder> {
  _$AuditRequest? _$v;

  int? _limitCommaOmitempty;
  int? get limitCommaOmitempty => _$this._limitCommaOmitempty;
  set limitCommaOmitempty(int? limitCommaOmitempty) =>
      _$this._limitCommaOmitempty = limitCommaOmitempty;

  AuditRequestBuilder() {
    AuditRequest._defaults(this);
  }

  AuditRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _limitCommaOmitempty = $v.limitCommaOmitempty;
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
          limitCommaOmitempty: limitCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
