// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'clear_cache_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ClearCacheRequest extends ClearCacheRequest {
  @override
  final String? prefix;

  factory _$ClearCacheRequest(
          [void Function(ClearCacheRequestBuilder)? updates]) =>
      (ClearCacheRequestBuilder()..update(updates))._build();

  _$ClearCacheRequest._({this.prefix}) : super._();
  @override
  ClearCacheRequest rebuild(void Function(ClearCacheRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ClearCacheRequestBuilder toBuilder() =>
      ClearCacheRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ClearCacheRequest &&
        prefix == other.prefix;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, prefix.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ClearCacheRequest')
          ..add('prefix', prefix))
        .toString();
  }
}

class ClearCacheRequestBuilder
    implements Builder<ClearCacheRequest, ClearCacheRequestBuilder> {
  _$ClearCacheRequest? _$v;

  String? _prefix;
  String? get prefix => _$this._prefix;
  set prefix(String? prefix) =>
      _$this._prefix = prefix;

  ClearCacheRequestBuilder() {
    ClearCacheRequest._defaults(this);
  }

  ClearCacheRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _prefix = $v.prefix;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ClearCacheRequest other) {
    _$v = other as _$ClearCacheRequest;
  }

  @override
  void update(void Function(ClearCacheRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ClearCacheRequest build() => _build();

  _$ClearCacheRequest _build() {
    final _$result = _$v ??
        _$ClearCacheRequest._(
          prefix: prefix,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
