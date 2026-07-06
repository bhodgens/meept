// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'vector_search_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$VectorSearchRequest extends VectorSearchRequest {
  @override
  final String query;
  @override
  final int? limit;
  @override
  final String? shardTypes;

  factory _$VectorSearchRequest(
          [void Function(VectorSearchRequestBuilder)? updates]) =>
      (VectorSearchRequestBuilder()..update(updates))._build();

  _$VectorSearchRequest._(
      {required this.query,
      this.limit,
      this.shardTypes})
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
        limit == other.limit &&
        shardTypes == other.shardTypes;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, query.hashCode);
    _$hash = $jc(_$hash, limit.hashCode);
    _$hash = $jc(_$hash, shardTypes.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'VectorSearchRequest')
          ..add('query', query)
          ..add('limit', limit)
          ..add('shardTypes', shardTypes))
        .toString();
  }
}

class VectorSearchRequestBuilder
    implements Builder<VectorSearchRequest, VectorSearchRequestBuilder> {
  _$VectorSearchRequest? _$v;

  String? _query;
  String? get query => _$this._query;
  set query(String? query) => _$this._query = query;

  int? _limit;
  int? get limit => _$this._limit;
  set limit(int? limit) =>
      _$this._limit = limit;

  String? _shardTypes;
  String? get shardTypes => _$this._shardTypes;
  set shardTypes(String? shardTypes) =>
      _$this._shardTypes = shardTypes;

  VectorSearchRequestBuilder() {
    VectorSearchRequest._defaults(this);
  }

  VectorSearchRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _query = $v.query;
      _limit = $v.limit;
      _shardTypes = $v.shardTypes;
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
          limit: limit,
          shardTypes: shardTypes,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
