// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'vector_search_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$VectorSearchRequest extends VectorSearchRequest {
  @override
  final String query;
  @override
  final int? limitCommaOmitempty;
  @override
  final String? shardTypesCommaOmitempty;

  factory _$VectorSearchRequest(
          [void Function(VectorSearchRequestBuilder)? updates]) =>
      (VectorSearchRequestBuilder()..update(updates))._build();

  _$VectorSearchRequest._(
      {required this.query,
      this.limitCommaOmitempty,
      this.shardTypesCommaOmitempty})
      : super._();
  @override
  VectorSearchRequest rebuild(
          void Function(VectorSearchRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  VectorSearchRequestBuilder toBuilder() =>
      VectorSearchRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is VectorSearchRequest &&
        query == other.query &&
        limitCommaOmitempty == other.limitCommaOmitempty &&
        shardTypesCommaOmitempty == other.shardTypesCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, query.hashCode);
    _$hash = $jc(_$hash, limitCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, shardTypesCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'VectorSearchRequest')
          ..add('query', query)
          ..add('limitCommaOmitempty', limitCommaOmitempty)
          ..add('shardTypesCommaOmitempty', shardTypesCommaOmitempty))
        .toString();
  }
}

class VectorSearchRequestBuilder
    implements Builder<VectorSearchRequest, VectorSearchRequestBuilder> {
  _$VectorSearchRequest? _$v;

  String? _query;
  String? get query => _$this._query;
  set query(String? query) => _$this._query = query;

  int? _limitCommaOmitempty;
  int? get limitCommaOmitempty => _$this._limitCommaOmitempty;
  set limitCommaOmitempty(int? limitCommaOmitempty) =>
      _$this._limitCommaOmitempty = limitCommaOmitempty;

  String? _shardTypesCommaOmitempty;
  String? get shardTypesCommaOmitempty => _$this._shardTypesCommaOmitempty;
  set shardTypesCommaOmitempty(String? shardTypesCommaOmitempty) =>
      _$this._shardTypesCommaOmitempty = shardTypesCommaOmitempty;

  VectorSearchRequestBuilder() {
    VectorSearchRequest._defaults(this);
  }

  VectorSearchRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _query = $v.query;
      _limitCommaOmitempty = $v.limitCommaOmitempty;
      _shardTypesCommaOmitempty = $v.shardTypesCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(VectorSearchRequest other) {
    _$v = other as _$VectorSearchRequest;
  }

  @override
  void update(void Function(VectorSearchRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  VectorSearchRequest build() => _build();

  _$VectorSearchRequest _build() {
    final _$result = _$v ??
        _$VectorSearchRequest._(
          query: BuiltValueNullFieldError.checkNotNull(
              query, r'VectorSearchRequest', 'query'),
          limitCommaOmitempty: limitCommaOmitempty,
          shardTypesCommaOmitempty: shardTypesCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
