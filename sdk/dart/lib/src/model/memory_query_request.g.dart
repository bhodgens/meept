// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'memory_query_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$MemoryQueryRequest extends MemoryQueryRequest {
  @override
  final String query;
  @override
  final int? limitCommaOmitempty;
  @override
  final String? categoryCommaOmitempty;

  factory _$MemoryQueryRequest(
          [void Function(MemoryQueryRequestBuilder)? updates]) =>
      (MemoryQueryRequestBuilder()..update(updates))._build();

  _$MemoryQueryRequest._(
      {required this.query,
      this.limitCommaOmitempty,
      this.categoryCommaOmitempty})
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
        limitCommaOmitempty == other.limitCommaOmitempty &&
        categoryCommaOmitempty == other.categoryCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, query.hashCode);
    _$hash = $jc(_$hash, limitCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, categoryCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'MemoryQueryRequest')
          ..add('query', query)
          ..add('limitCommaOmitempty', limitCommaOmitempty)
          ..add('categoryCommaOmitempty', categoryCommaOmitempty))
        .toString();
  }
}

class MemoryQueryRequestBuilder
    implements Builder<MemoryQueryRequest, MemoryQueryRequestBuilder> {
  _$MemoryQueryRequest? _$v;

  String? _query;
  String? get query => _$this._query;
  set query(String? query) => _$this._query = query;

  int? _limitCommaOmitempty;
  int? get limitCommaOmitempty => _$this._limitCommaOmitempty;
  set limitCommaOmitempty(int? limitCommaOmitempty) =>
      _$this._limitCommaOmitempty = limitCommaOmitempty;

  String? _categoryCommaOmitempty;
  String? get categoryCommaOmitempty => _$this._categoryCommaOmitempty;
  set categoryCommaOmitempty(String? categoryCommaOmitempty) =>
      _$this._categoryCommaOmitempty = categoryCommaOmitempty;

  MemoryQueryRequestBuilder() {
    MemoryQueryRequest._defaults(this);
  }

  MemoryQueryRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _query = $v.query;
      _limitCommaOmitempty = $v.limitCommaOmitempty;
      _categoryCommaOmitempty = $v.categoryCommaOmitempty;
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
          limitCommaOmitempty: limitCommaOmitempty,
          categoryCommaOmitempty: categoryCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
