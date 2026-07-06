// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'list_sessions_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ListSessionsRequest extends ListSessionsRequest {
  @override
  final int? limit;

  factory _$ListSessionsRequest(
          [void Function(ListSessionsRequestBuilder)? updates]) =>
      (ListSessionsRequestBuilder()..update(updates))._build();

  _$ListSessionsRequest._({this.limit}) : super._();
  @override
  ListSessionsRequest rebuild(
          void Function(ListSessionsRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ListSessionsRequestBuilder toBuilder() =>
      ListSessionsRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ListSessionsRequest &&
        limit == other.limit;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, limit.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ListSessionsRequest')
          ..add('limit', limit))
        .toString();
  }
}

class ListSessionsRequestBuilder
    implements Builder<ListSessionsRequest, ListSessionsRequestBuilder> {
  _$ListSessionsRequest? _$v;

  int? _limit;
  int? get limit => _$this._limit;
  set limit(int? limit) =>
      _$this._limit = limit;

  ListSessionsRequestBuilder() {
    ListSessionsRequest._defaults(this);
  }

  ListSessionsRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _limit = $v.limit;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ListSessionsRequest other) {
    _$v = other as _$ListSessionsRequest;
  }

  @override
  void update(void Function(ListSessionsRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ListSessionsRequest build() => _build();

  _$ListSessionsRequest _build() {
    final _$result = _$v ??
        _$ListSessionsRequest._(
          limit: limit,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
