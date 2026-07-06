// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'search_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SearchRequest extends SearchRequest {
  @override
  final String query;
  @override
  final String? scope;
  @override
  final int? limit;

  factory _$SearchRequest([void Function(SearchRequestBuilder)? updates]) =>
      (SearchRequestBuilder()..update(updates))._build();

  _$SearchRequest._(
      {required this.query, this.scope, this.limit})
      : super._();
  @override
  SearchRequest rebuild(void Function(SearchRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SearchRequestBuilder toBuilder() => SearchRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SearchRequest &&
        query == other.query &&
        scope == other.scope &&
        limit == other.limit;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, query.hashCode);
    _$hash = $jc(_$hash, scope.hashCode);
    _$hash = $jc(_$hash, limit.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SearchRequest')
          ..add('query', query)
          ..add('scope', scope)
          ..add('limit', limit))
        .toString();
  }
}

class SearchRequestBuilder
    implements Builder<SearchRequest, SearchRequestBuilder> {
  _$SearchRequest? _$v;

  String? _query;
  String? get query => _$this._query;
  set query(String? query) => _$this._query = query;

  String? _scope;
  String? get scope => _$this._scope;
  set scope(String? scope) =>
      _$this._scope = scope;

  int? _limit;
  int? get limit => _$this._limit;
  set limit(int? limit) =>
      _$this._limit = limit;

  SearchRequestBuilder() {
    SearchRequest._defaults(this);
  }

  SearchRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _query = $v.query;
      _scope = $v.scope;
      _limit = $v.limit;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SearchRequest other) {
    _$v = other as _$SearchRequest;
  }

  @override
  void update(void Function(SearchRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SearchRequest build() => _build();

  _$SearchRequest _build() {
    final _$result = _$v ??
        _$SearchRequest._(
          query: BuiltValueNullFieldError.checkNotNull(
              query, r'SearchRequest', 'query'),
          scope: scope,
          limit: limit,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
