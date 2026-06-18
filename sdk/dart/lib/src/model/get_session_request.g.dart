// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'get_session_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$GetSessionRequest extends GetSessionRequest {
  @override
  final String id;

  factory _$GetSessionRequest(
          [void Function(GetSessionRequestBuilder)? updates]) =>
      (GetSessionRequestBuilder()..update(updates))._build();

  _$GetSessionRequest._({required this.id}) : super._();
  @override
  GetSessionRequest rebuild(void Function(GetSessionRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  GetSessionRequestBuilder toBuilder() =>
      GetSessionRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is GetSessionRequest && id == other.id;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'GetSessionRequest')..add('id', id))
        .toString();
  }
}

class GetSessionRequestBuilder
    implements Builder<GetSessionRequest, GetSessionRequestBuilder> {
  _$GetSessionRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  GetSessionRequestBuilder() {
    GetSessionRequest._defaults(this);
  }

  GetSessionRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(GetSessionRequest other) {
    _$v = other as _$GetSessionRequest;
  }

  @override
  void update(void Function(GetSessionRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  GetSessionRequest build() => _build();

  _$GetSessionRequest _build() {
    final _$result = _$v ??
        _$GetSessionRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'GetSessionRequest', 'id'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
