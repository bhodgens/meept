// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'get_messages_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$GetMessagesRequest extends GetMessagesRequest {
  @override
  final String id;
  @override
  final int? offset;
  @override
  final int? limit;

  factory _$GetMessagesRequest(
          [void Function(GetMessagesRequestBuilder)? updates]) =>
      (GetMessagesRequestBuilder()..update(updates))._build();

  _$GetMessagesRequest._(
      {required this.id, this.offset, this.limit})
      : super._();
  @override
  GetMessagesRequest rebuild(
          void Function(GetMessagesRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  GetMessagesRequestBuilder toBuilder() =>
      GetMessagesRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is GetMessagesRequest &&
        id == other.id &&
        offset == other.offset &&
        limit == other.limit;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, offset.hashCode);
    _$hash = $jc(_$hash, limit.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'GetMessagesRequest')
          ..add('id', id)
          ..add('offset', offset)
          ..add('limit', limit))
        .toString();
  }
}

class GetMessagesRequestBuilder
    implements Builder<GetMessagesRequest, GetMessagesRequestBuilder> {
  _$GetMessagesRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  int? _offset;
  int? get offset => _$this._offset;
  set offset(int? offset) =>
      _$this._offset = offset;

  int? _limit;
  int? get limit => _$this._limit;
  set limit(int? limit) =>
      _$this._limit = limit;

  GetMessagesRequestBuilder() {
    GetMessagesRequest._defaults(this);
  }

  GetMessagesRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _offset = $v.offset;
      _limit = $v.limit;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(GetMessagesRequest other) {
    _$v = other as _$GetMessagesRequest;
  }

  @override
  void update(void Function(GetMessagesRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  GetMessagesRequest build() => _build();

  _$GetMessagesRequest _build() {
    final _$result = _$v ??
        _$GetMessagesRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'GetMessagesRequest', 'id'),
          offset: offset,
          limit: limit,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
