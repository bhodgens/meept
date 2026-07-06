// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'memory_query_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$MemoryQueryRequest extends MemoryQueryRequest {
  @override
  final String query;
  @override
  final int? limit;
  @override
  final String? category;

  factory _$MemoryQueryRequest(
          [void Function(MemoryQueryRequestBuilder)? updates]) =>
      (MemoryQueryRequestBuilder()..update(updates))._build();

  _$MemoryQueryRequest._(
      {required this.query,
      this.limit,
      this.category})
      : super._();
  @override
  MemoryQueryRequest rebuild(
          void Function(MemoryQueryRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  MemoryQueryRequestBuilder toBuilder() =>
      MemoryQueryRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is MemoryQueryRequest &&
        query == other.query &&
        limit == other.limit &&
        category == other.category;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, query.hashCode);
    _$hash = $jc(_$hash, limit.hashCode);
    _$hash = $jc(_$hash, category.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'MemoryQueryRequest')
          ..add('query', query)
          ..add('limit', limit)
          ..add('category', category))
        .toString();
  }
}

class MemoryQueryRequestBuilder
    implements Builder<MemoryQueryRequest, MemoryQueryRequestBuilder> {
  _$MemoryQueryRequest? _$v;

  String? _query;
  String? get query => _$this._query;
  set query(String? query) => _$this._query = query;

  int? _limit;
  int? get limit => _$this._limit;
  set limit(int? limit) =>
      _$this._limit = limit;

  String? _category;
  String? get category => _$this._category;
  set category(String? category) =>
      _$this._category = category;

  MemoryQueryRequestBuilder() {
    MemoryQueryRequest._defaults(this);
  }

  MemoryQueryRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _query = $v.query;
      _limit = $v.limit;
      _category = $v.category;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(MemoryQueryRequest other) {
    _$v = other as _$MemoryQueryRequest;
  }

  @override
  void update(void Function(MemoryQueryRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  MemoryQueryRequest build() => _build();

  _$MemoryQueryRequest _build() {
    final _$result = _$v ??
        _$MemoryQueryRequest._(
          query: BuiltValueNullFieldError.checkNotNull(
              query, r'MemoryQueryRequest', 'query'),
          limit: limit,
          category: category,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
