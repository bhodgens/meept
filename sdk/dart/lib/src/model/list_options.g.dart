// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'list_options.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ListOptions extends ListOptions {
  @override
  final int? limit;
  @override
  final int? offset;
  @override
  final String? filter;

  factory _$ListOptions([void Function(ListOptionsBuilder)? updates]) =>
      (ListOptionsBuilder()..update(updates))._build();

  _$ListOptions._(
      {this.limit,
      this.offset,
      this.filter})
      : super._();
  @override
  ListOptions rebuild(void Function(ListOptionsBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ListOptionsBuilder toBuilder() => ListOptionsBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ListOptions &&
        limit == other.limit &&
        offset == other.offset &&
        filter == other.filter;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, limit.hashCode);
    _$hash = $jc(_$hash, offset.hashCode);
    _$hash = $jc(_$hash, filter.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ListOptions')
          ..add('limit', limit)
          ..add('offset', offset)
          ..add('filter', filter))
        .toString();
  }
}

class ListOptionsBuilder implements Builder<ListOptions, ListOptionsBuilder> {
  _$ListOptions? _$v;

  int? _limit;
  int? get limit => _$this._limit;
  set limit(int? limit) =>
      _$this._limit = limit;

  int? _offset;
  int? get offset => _$this._offset;
  set offset(int? offset) =>
      _$this._offset = offset;

  String? _filter;
  String? get filter => _$this._filter;
  set filter(String? filter) =>
      _$this._filter = filter;

  ListOptionsBuilder() {
    ListOptions._defaults(this);
  }

  ListOptionsBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _limit = $v.limit;
      _offset = $v.offset;
      _filter = $v.filter;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ListOptions other) {
    _$v = other as _$ListOptions;
  }

  @override
  void update(void Function(ListOptionsBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ListOptions build() => _build();

  _$ListOptions _build() {
    final _$result = _$v ??
        _$ListOptions._(
          limit: limit,
          offset: offset,
          filter: filter,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
