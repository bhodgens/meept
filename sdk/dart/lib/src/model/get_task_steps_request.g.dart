// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'get_task_steps_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$GetTaskStepsRequest extends GetTaskStepsRequest {
  @override
  final String id;

  factory _$GetTaskStepsRequest(
          [void Function(GetTaskStepsRequestBuilder)? updates]) =>
      (GetTaskStepsRequestBuilder()..update(updates))._build();

  _$GetTaskStepsRequest._({required this.id}) : super._();
  @override
  GetTaskStepsRequest rebuild(
          void Function(GetTaskStepsRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  GetTaskStepsRequestBuilder toBuilder() =>
      GetTaskStepsRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is GetTaskStepsRequest && id == other.id;
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
    return (newBuiltValueToStringHelper(r'GetTaskStepsRequest')..add('id', id))
        .toString();
  }
}

class GetTaskStepsRequestBuilder
    implements Builder<GetTaskStepsRequest, GetTaskStepsRequestBuilder> {
  _$GetTaskStepsRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  GetTaskStepsRequestBuilder() {
    GetTaskStepsRequest._defaults(this);
  }

  GetTaskStepsRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(GetTaskStepsRequest other) {
    _$v = other as _$GetTaskStepsRequest;
  }

  @override
  void update(void Function(GetTaskStepsRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  GetTaskStepsRequest build() => _build();

  _$GetTaskStepsRequest _build() {
    final _$result = _$v ??
        _$GetTaskStepsRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'GetTaskStepsRequest', 'id'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
