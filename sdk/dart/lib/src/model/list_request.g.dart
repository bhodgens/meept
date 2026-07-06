// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'list_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ListRequest extends ListRequest {
  @override
  final String? state;
  @override
  final int? limit;

  factory _$ListRequest([void Function(ListRequestBuilder)? updates]) =>
      (ListRequestBuilder()..update(updates))._build();

  _$ListRequest._({this.state, this.limit})
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
        state == other.state &&
        limit == other.limit;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, state.hashCode);
    _$hash = $jc(_$hash, limit.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ListRequest')
          ..add('state', state)
          ..add('limit', limit))
        .toString();
  }
}

class ListRequestBuilder implements Builder<ListRequest, ListRequestBuilder> {
  _$ListRequest? _$v;

  String? _state;
  String? get state => _$this._state;
  set state(String? state) =>
      _$this._state = state;

  int? _limit;
  int? get limit => _$this._limit;
  set limit(int? limit) =>
      _$this._limit = limit;

  ListRequestBuilder() {
    ListRequest._defaults(this);
  }

  ListRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _state = $v.state;
      _limit = $v.limit;
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
          state: state,
          limit: limit,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
