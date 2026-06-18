// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'cancel_task_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CancelTaskRequest extends CancelTaskRequest {
  @override
  final String id;

  factory _$CancelTaskRequest(
          [void Function(CancelTaskRequestBuilder)? updates]) =>
      (CancelTaskRequestBuilder()..update(updates))._build();

  _$CancelTaskRequest._({required this.id}) : super._();
  @override
  CancelTaskRequest rebuild(void Function(CancelTaskRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CancelTaskRequestBuilder toBuilder() =>
      CancelTaskRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CancelTaskRequest && id == other.id;
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
    return (newBuiltValueToStringHelper(r'CancelTaskRequest')..add('id', id))
        .toString();
  }
}

class CancelTaskRequestBuilder
    implements Builder<CancelTaskRequest, CancelTaskRequestBuilder> {
  _$CancelTaskRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  CancelTaskRequestBuilder() {
    CancelTaskRequest._defaults(this);
  }

  CancelTaskRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CancelTaskRequest other) {
    _$v = other as _$CancelTaskRequest;
  }

  @override
  void update(void Function(CancelTaskRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CancelTaskRequest build() => _build();

  _$CancelTaskRequest _build() {
    final _$result = _$v ??
        _$CancelTaskRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'CancelTaskRequest', 'id'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
