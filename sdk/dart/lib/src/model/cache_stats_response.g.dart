// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'cache_stats_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CacheStatsResponse extends CacheStatsResponse {
  @override
  final int hits;
  @override
  final int misses;
  @override
  final int size;

  factory _$CacheStatsResponse(
          [void Function(CacheStatsResponseBuilder)? updates]) =>
      (CacheStatsResponseBuilder()..update(updates))._build();

  _$CacheStatsResponse._(
      {required this.hits, required this.misses, required this.size})
      : super._();
  @override
  CacheStatsResponse rebuild(
          void Function(CacheStatsResponseBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CacheStatsResponseBuilder toBuilder() =>
      CacheStatsResponseBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CacheStatsResponse &&
        hits == other.hits &&
        misses == other.misses &&
        size == other.size;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, hits.hashCode);
    _$hash = $jc(_$hash, misses.hashCode);
    _$hash = $jc(_$hash, size.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CacheStatsResponse')
          ..add('hits', hits)
          ..add('misses', misses)
          ..add('size', size))
        .toString();
  }
}

class CacheStatsResponseBuilder
    implements Builder<CacheStatsResponse, CacheStatsResponseBuilder> {
  _$CacheStatsResponse? _$v;

  int? _hits;
  int? get hits => _$this._hits;
  set hits(int? hits) => _$this._hits = hits;

  int? _misses;
  int? get misses => _$this._misses;
  set misses(int? misses) => _$this._misses = misses;

  int? _size;
  int? get size => _$this._size;
  set size(int? size) => _$this._size = size;

  CacheStatsResponseBuilder() {
    CacheStatsResponse._defaults(this);
  }

  CacheStatsResponseBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _hits = $v.hits;
      _misses = $v.misses;
      _size = $v.size;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CacheStatsResponse other) {
    _$v = other as _$CacheStatsResponse;
  }

  @override
  void update(void Function(CacheStatsResponseBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CacheStatsResponse build() => _build();

  _$CacheStatsResponse _build() {
    final _$result = _$v ??
        _$CacheStatsResponse._(
          hits: BuiltValueNullFieldError.checkNotNull(
              hits, r'CacheStatsResponse', 'hits'),
          misses: BuiltValueNullFieldError.checkNotNull(
              misses, r'CacheStatsResponse', 'misses'),
          size: BuiltValueNullFieldError.checkNotNull(
              size, r'CacheStatsResponse', 'size'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
