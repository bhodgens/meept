// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'delete_session_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$DeleteSessionRequest extends DeleteSessionRequest {
  @override
  final String id;

  factory _$DeleteSessionRequest(
          [void Function(DeleteSessionRequestBuilder)? updates]) =>
      (DeleteSessionRequestBuilder()..update(updates))._build();

  _$DeleteSessionRequest._({required this.id}) : super._();
  @override
  DeleteSessionRequest rebuild(
          void Function(DeleteSessionRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  DeleteSessionRequestBuilder toBuilder() =>
      DeleteSessionRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is DeleteSessionRequest && id == other.id;
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
    return (newBuiltValueToStringHelper(r'DeleteSessionRequest')..add('id', id))
        .toString();
  }
}

class DeleteSessionRequestBuilder
    implements Builder<DeleteSessionRequest, DeleteSessionRequestBuilder> {
  _$DeleteSessionRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  DeleteSessionRequestBuilder() {
    DeleteSessionRequest._defaults(this);
  }

  DeleteSessionRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(DeleteSessionRequest other) {
    _$v = other as _$DeleteSessionRequest;
  }

  @override
  void update(void Function(DeleteSessionRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  DeleteSessionRequest build() => _build();

  _$DeleteSessionRequest _build() {
    final _$result = _$v ??
        _$DeleteSessionRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'DeleteSessionRequest', 'id'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
