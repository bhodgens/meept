// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'list_options.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ListOptions extends ListOptions {
  @override
  final int? limitCommaOmitempty;
  @override
  final int? offsetCommaOmitempty;
  @override
  final String? filterCommaOmitempty;

  factory _$ListOptions([void Function(ListOptionsBuilder)? updates]) =>
      (ListOptionsBuilder()..update(updates))._build();

  _$ListOptions._(
      {this.limitCommaOmitempty,
      this.offsetCommaOmitempty,
      this.filterCommaOmitempty})
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
        limitCommaOmitempty == other.limitCommaOmitempty &&
        offsetCommaOmitempty == other.offsetCommaOmitempty &&
        filterCommaOmitempty == other.filterCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, limitCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, offsetCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, filterCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ListOptions')
          ..add('limitCommaOmitempty', limitCommaOmitempty)
          ..add('offsetCommaOmitempty', offsetCommaOmitempty)
          ..add('filterCommaOmitempty', filterCommaOmitempty))
        .toString();
  }
}

class ListOptionsBuilder implements Builder<ListOptions, ListOptionsBuilder> {
  _$ListOptions? _$v;

  int? _limitCommaOmitempty;
  int? get limitCommaOmitempty => _$this._limitCommaOmitempty;
  set limitCommaOmitempty(int? limitCommaOmitempty) =>
      _$this._limitCommaOmitempty = limitCommaOmitempty;

  int? _offsetCommaOmitempty;
  int? get offsetCommaOmitempty => _$this._offsetCommaOmitempty;
  set offsetCommaOmitempty(int? offsetCommaOmitempty) =>
      _$this._offsetCommaOmitempty = offsetCommaOmitempty;

  String? _filterCommaOmitempty;
  String? get filterCommaOmitempty => _$this._filterCommaOmitempty;
  set filterCommaOmitempty(String? filterCommaOmitempty) =>
      _$this._filterCommaOmitempty = filterCommaOmitempty;

  ListOptionsBuilder() {
    ListOptions._defaults(this);
  }

  ListOptionsBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _limitCommaOmitempty = $v.limitCommaOmitempty;
      _offsetCommaOmitempty = $v.offsetCommaOmitempty;
      _filterCommaOmitempty = $v.filterCommaOmitempty;
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
          limitCommaOmitempty: limitCommaOmitempty,
          offsetCommaOmitempty: offsetCommaOmitempty,
          filterCommaOmitempty: filterCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
