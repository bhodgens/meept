// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'list_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ListRequest extends ListRequest {
  @override
  final String? stateCommaOmitempty;
  @override
  final int? limitCommaOmitempty;

  factory _$ListRequest([void Function(ListRequestBuilder)? updates]) =>
      (ListRequestBuilder()..update(updates))._build();

  _$ListRequest._({this.stateCommaOmitempty, this.limitCommaOmitempty})
      : super._();
  @override
  ListRequest rebuild(void Function(ListRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ListRequestBuilder toBuilder() => ListRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ListRequest &&
        stateCommaOmitempty == other.stateCommaOmitempty &&
        limitCommaOmitempty == other.limitCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, stateCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, limitCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ListRequest')
          ..add('stateCommaOmitempty', stateCommaOmitempty)
          ..add('limitCommaOmitempty', limitCommaOmitempty))
        .toString();
  }
}

class ListRequestBuilder implements Builder<ListRequest, ListRequestBuilder> {
  _$ListRequest? _$v;

  String? _stateCommaOmitempty;
  String? get stateCommaOmitempty => _$this._stateCommaOmitempty;
  set stateCommaOmitempty(String? stateCommaOmitempty) =>
      _$this._stateCommaOmitempty = stateCommaOmitempty;

  int? _limitCommaOmitempty;
  int? get limitCommaOmitempty => _$this._limitCommaOmitempty;
  set limitCommaOmitempty(int? limitCommaOmitempty) =>
      _$this._limitCommaOmitempty = limitCommaOmitempty;

  ListRequestBuilder() {
    ListRequest._defaults(this);
  }

  ListRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _stateCommaOmitempty = $v.stateCommaOmitempty;
      _limitCommaOmitempty = $v.limitCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ListRequest other) {
    _$v = other as _$ListRequest;
  }

  @override
  void update(void Function(ListRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ListRequest build() => _build();

  _$ListRequest _build() {
    final _$result = _$v ??
        _$ListRequest._(
          stateCommaOmitempty: stateCommaOmitempty,
          limitCommaOmitempty: limitCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
