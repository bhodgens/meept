// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'paginated_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$PaginatedResponse extends PaginatedResponse {
  @override
  final BuiltList<String>? items;
  @override
  final int total;
  @override
  final bool hasMore;
  @override
  final int? nextOffsetCommaOmitempty;

  factory _$PaginatedResponse(
          [void Function(PaginatedResponseBuilder)? updates]) =>
      (PaginatedResponseBuilder()..update(updates))._build();

  _$PaginatedResponse._(
      {this.items,
      required this.total,
      required this.hasMore,
      this.nextOffsetCommaOmitempty})
      : super._();
  @override
  PaginatedResponse rebuild(void Function(PaginatedResponseBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  PaginatedResponseBuilder toBuilder() =>
      PaginatedResponseBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is PaginatedResponse &&
        items == other.items &&
        total == other.total &&
        hasMore == other.hasMore &&
        nextOffsetCommaOmitempty == other.nextOffsetCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, items.hashCode);
    _$hash = $jc(_$hash, total.hashCode);
    _$hash = $jc(_$hash, hasMore.hashCode);
    _$hash = $jc(_$hash, nextOffsetCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'PaginatedResponse')
          ..add('items', items)
          ..add('total', total)
          ..add('hasMore', hasMore)
          ..add('nextOffsetCommaOmitempty', nextOffsetCommaOmitempty))
        .toString();
  }
}

class PaginatedResponseBuilder
    implements Builder<PaginatedResponse, PaginatedResponseBuilder> {
  _$PaginatedResponse? _$v;

  ListBuilder<String>? _items;
  ListBuilder<String> get items => _$this._items ??= ListBuilder<String>();
  set items(ListBuilder<String>? items) => _$this._items = items;

  int? _total;
  int? get total => _$this._total;
  set total(int? total) => _$this._total = total;

  bool? _hasMore;
  bool? get hasMore => _$this._hasMore;
  set hasMore(bool? hasMore) => _$this._hasMore = hasMore;

  int? _nextOffsetCommaOmitempty;
  int? get nextOffsetCommaOmitempty => _$this._nextOffsetCommaOmitempty;
  set nextOffsetCommaOmitempty(int? nextOffsetCommaOmitempty) =>
      _$this._nextOffsetCommaOmitempty = nextOffsetCommaOmitempty;

  PaginatedResponseBuilder() {
    PaginatedResponse._defaults(this);
  }

  PaginatedResponseBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _items = $v.items?.toBuilder();
      _total = $v.total;
      _hasMore = $v.hasMore;
      _nextOffsetCommaOmitempty = $v.nextOffsetCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(PaginatedResponse other) {
    _$v = other as _$PaginatedResponse;
  }

  @override
  void update(void Function(PaginatedResponseBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  PaginatedResponse build() => _build();

  _$PaginatedResponse _build() {
    _$PaginatedResponse _$result;
    try {
      _$result = _$v ??
          _$PaginatedResponse._(
            items: _items?.build(),
            total: BuiltValueNullFieldError.checkNotNull(
                total, r'PaginatedResponse', 'total'),
            hasMore: BuiltValueNullFieldError.checkNotNull(
                hasMore, r'PaginatedResponse', 'hasMore'),
            nextOffsetCommaOmitempty: nextOffsetCommaOmitempty,
          );
    } catch (_) {
      late String _$failedField;
      try {
        _$failedField = 'items';
        _items?.build();
      } catch (e) {
        throw BuiltValueNestedFieldError(
            r'PaginatedResponse', _$failedField, e.toString());
      }
      rethrow;
    }
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
