// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'vector_search_result.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$VectorSearchResult extends VectorSearchResult {
  @override
  final String memoryId;
  @override
  final String content;
  @override
  final String? metadata;
  @override
  final num relevanceScore;
  @override
  final num vectorSimilarity;

  factory _$VectorSearchResult(
          [void Function(VectorSearchResultBuilder)? updates]) =>
      (VectorSearchResultBuilder()..update(updates))._build();

  _$VectorSearchResult._(
      {required this.memoryId,
      required this.content,
      this.metadata,
      required this.relevanceScore,
      required this.vectorSimilarity})
      : super._();
  @override
  VectorSearchResult rebuild(
          void Function(VectorSearchResultBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  VectorSearchResultBuilder toBuilder() =>
      VectorSearchResultBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is VectorSearchResult &&
        memoryId == other.memoryId &&
        content == other.content &&
        metadata == other.metadata &&
        relevanceScore == other.relevanceScore &&
        vectorSimilarity == other.vectorSimilarity;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, memoryId.hashCode);
    _$hash = $jc(_$hash, content.hashCode);
    _$hash = $jc(_$hash, metadata.hashCode);
    _$hash = $jc(_$hash, relevanceScore.hashCode);
    _$hash = $jc(_$hash, vectorSimilarity.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'VectorSearchResult')
          ..add('memoryId', memoryId)
          ..add('content', content)
          ..add('metadata', metadata)
          ..add('relevanceScore', relevanceScore)
          ..add('vectorSimilarity', vectorSimilarity))
        .toString();
  }
}

class VectorSearchResultBuilder
    implements Builder<VectorSearchResult, VectorSearchResultBuilder> {
  _$VectorSearchResult? _$v;

  String? _memoryId;
  String? get memoryId => _$this._memoryId;
  set memoryId(String? memoryId) => _$this._memoryId = memoryId;

  String? _content;
  String? get content => _$this._content;
  set content(String? content) => _$this._content = content;

  String? _metadata;
  String? get metadata => _$this._metadata;
  set metadata(String? metadata) =>
      _$this._metadata = metadata;

  num? _relevanceScore;
  num? get relevanceScore => _$this._relevanceScore;
  set relevanceScore(num? relevanceScore) =>
      _$this._relevanceScore = relevanceScore;

  num? _vectorSimilarity;
  num? get vectorSimilarity => _$this._vectorSimilarity;
  set vectorSimilarity(num? vectorSimilarity) =>
      _$this._vectorSimilarity = vectorSimilarity;

  VectorSearchResultBuilder() {
    VectorSearchResult._defaults(this);
  }

  VectorSearchResultBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _memoryId = $v.memoryId;
      _content = $v.content;
      _metadata = $v.metadata;
      _relevanceScore = $v.relevanceScore;
      _vectorSimilarity = $v.vectorSimilarity;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(VectorSearchResult other) {
    _$v = other as _$VectorSearchResult;
  }

  @override
  void update(void Function(VectorSearchResultBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  VectorSearchResult build() => _build();

  _$VectorSearchResult _build() {
    final _$result = _$v ??
        _$VectorSearchResult._(
          memoryId: BuiltValueNullFieldError.checkNotNull(
              memoryId, r'VectorSearchResult', 'memoryId'),
          content: BuiltValueNullFieldError.checkNotNull(
              content, r'VectorSearchResult', 'content'),
          metadata: metadata,
          relevanceScore: BuiltValueNullFieldError.checkNotNull(
              relevanceScore, r'VectorSearchResult', 'relevanceScore'),
          vectorSimilarity: BuiltValueNullFieldError.checkNotNull(
              vectorSimilarity, r'VectorSearchResult', 'vectorSimilarity'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
