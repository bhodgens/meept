// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'clear_cache_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ClearCacheRequest extends ClearCacheRequest {
  @override
  final String? prefixCommaOmitempty;

  factory _$ClearCacheRequest(
          [void Function(ClearCacheRequestBuilder)? updates]) =>
      (ClearCacheRequestBuilder()..update(updates))._build();

  _$ClearCacheRequest._({this.prefixCommaOmitempty}) : super._();
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
        prefixCommaOmitempty == other.prefixCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, prefixCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ClearCacheRequest')
          ..add('prefixCommaOmitempty', prefixCommaOmitempty))
        .toString();
  }
}

class ClearCacheRequestBuilder
    implements Builder<ClearCacheRequest, ClearCacheRequestBuilder> {
  _$ClearCacheRequest? _$v;

  String? _prefixCommaOmitempty;
  String? get prefixCommaOmitempty => _$this._prefixCommaOmitempty;
  set prefixCommaOmitempty(String? prefixCommaOmitempty) =>
      _$this._prefixCommaOmitempty = prefixCommaOmitempty;

  ClearCacheRequestBuilder() {
    ClearCacheRequest._defaults(this);
  }

  ClearCacheRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _prefixCommaOmitempty = $v.prefixCommaOmitempty;
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
          prefixCommaOmitempty: prefixCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
