// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'delete_task_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$DeleteTaskRequest extends DeleteTaskRequest {
  @override
  final String id;

  factory _$DeleteTaskRequest(
          [void Function(DeleteTaskRequestBuilder)? updates]) =>
      (DeleteTaskRequestBuilder()..update(updates))._build();

  _$DeleteTaskRequest._({required this.id}) : super._();
  @override
  DeleteTaskRequest rebuild(void Function(DeleteTaskRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  DeleteTaskRequestBuilder toBuilder() =>
      DeleteTaskRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is DeleteTaskRequest && id == other.id;
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
    return (newBuiltValueToStringHelper(r'DeleteTaskRequest')..add('id', id))
        .toString();
  }
}

class DeleteTaskRequestBuilder
    implements Builder<DeleteTaskRequest, DeleteTaskRequestBuilder> {
  _$DeleteTaskRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  DeleteTaskRequestBuilder() {
    DeleteTaskRequest._defaults(this);
  }

  DeleteTaskRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(DeleteTaskRequest other) {
    _$v = other as _$DeleteTaskRequest;
  }

  @override
  void update(void Function(DeleteTaskRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  DeleteTaskRequest build() => _build();

  _$DeleteTaskRequest _build() {
    final _$result = _$v ??
        _$DeleteTaskRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'DeleteTaskRequest', 'id'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
