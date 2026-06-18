// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'search_result.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SearchResult extends SearchResult {
  @override
  final String type;
  @override
  final String id;
  @override
  final String title;
  @override
  final String snippet;
  @override
  final num relevance;

  factory _$SearchResult([void Function(SearchResultBuilder)? updates]) =>
      (SearchResultBuilder()..update(updates))._build();

  _$SearchResult._(
      {required this.type,
      required this.id,
      required this.title,
      required this.snippet,
      required this.relevance})
      : super._();
  @override
  SearchResult rebuild(void Function(SearchResultBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SearchResultBuilder toBuilder() => SearchResultBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SearchResult &&
        type == other.type &&
        id == other.id &&
        title == other.title &&
        snippet == other.snippet &&
        relevance == other.relevance;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, type.hashCode);
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, title.hashCode);
    _$hash = $jc(_$hash, snippet.hashCode);
    _$hash = $jc(_$hash, relevance.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SearchResult')
          ..add('type', type)
          ..add('id', id)
          ..add('title', title)
          ..add('snippet', snippet)
          ..add('relevance', relevance))
        .toString();
  }
}

class SearchResultBuilder
    implements Builder<SearchResult, SearchResultBuilder> {
  _$SearchResult? _$v;

  String? _type;
  String? get type => _$this._type;
  set type(String? type) => _$this._type = type;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _title;
  String? get title => _$this._title;
  set title(String? title) => _$this._title = title;

  String? _snippet;
  String? get snippet => _$this._snippet;
  set snippet(String? snippet) => _$this._snippet = snippet;

  num? _relevance;
  num? get relevance => _$this._relevance;
  set relevance(num? relevance) => _$this._relevance = relevance;

  SearchResultBuilder() {
    SearchResult._defaults(this);
  }

  SearchResultBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _type = $v.type;
      _id = $v.id;
      _title = $v.title;
      _snippet = $v.snippet;
      _relevance = $v.relevance;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SearchResult other) {
    _$v = other as _$SearchResult;
  }

  @override
  void update(void Function(SearchResultBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SearchResult build() => _build();

  _$SearchResult _build() {
    final _$result = _$v ??
        _$SearchResult._(
          type: BuiltValueNullFieldError.checkNotNull(
              type, r'SearchResult', 'type'),
          id: BuiltValueNullFieldError.checkNotNull(id, r'SearchResult', 'id'),
          title: BuiltValueNullFieldError.checkNotNull(
              title, r'SearchResult', 'title'),
          snippet: BuiltValueNullFieldError.checkNotNull(
              snippet, r'SearchResult', 'snippet'),
          relevance: BuiltValueNullFieldError.checkNotNull(
              relevance, r'SearchResult', 'relevance'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
