// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'update_task_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$UpdateTaskRequest extends UpdateTaskRequest {
  @override
  final String id;
  @override
  final String? stateCommaOmitempty;
  @override
  final String? nameCommaOmitempty;

  factory _$UpdateTaskRequest(
          [void Function(UpdateTaskRequestBuilder)? updates]) =>
      (UpdateTaskRequestBuilder()..update(updates))._build();

  _$UpdateTaskRequest._(
      {required this.id, this.stateCommaOmitempty, this.nameCommaOmitempty})
      : super._();
  @override
  UpdateTaskRequest rebuild(void Function(UpdateTaskRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  UpdateTaskRequestBuilder toBuilder() =>
      UpdateTaskRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is UpdateTaskRequest &&
        id == other.id &&
        stateCommaOmitempty == other.stateCommaOmitempty &&
        nameCommaOmitempty == other.nameCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, stateCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, nameCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'UpdateTaskRequest')
          ..add('id', id)
          ..add('stateCommaOmitempty', stateCommaOmitempty)
          ..add('nameCommaOmitempty', nameCommaOmitempty))
        .toString();
  }
}

class UpdateTaskRequestBuilder
    implements Builder<UpdateTaskRequest, UpdateTaskRequestBuilder> {
  _$UpdateTaskRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _stateCommaOmitempty;
  String? get stateCommaOmitempty => _$this._stateCommaOmitempty;
  set stateCommaOmitempty(String? stateCommaOmitempty) =>
      _$this._stateCommaOmitempty = stateCommaOmitempty;

  String? _nameCommaOmitempty;
  String? get nameCommaOmitempty => _$this._nameCommaOmitempty;
  set nameCommaOmitempty(String? nameCommaOmitempty) =>
      _$this._nameCommaOmitempty = nameCommaOmitempty;

  UpdateTaskRequestBuilder() {
    UpdateTaskRequest._defaults(this);
  }

  UpdateTaskRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _stateCommaOmitempty = $v.stateCommaOmitempty;
      _nameCommaOmitempty = $v.nameCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(UpdateTaskRequest other) {
    _$v = other as _$UpdateTaskRequest;
  }

  @override
  void update(void Function(UpdateTaskRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  UpdateTaskRequest build() => _build();

  _$UpdateTaskRequest _build() {
    final _$result = _$v ??
        _$UpdateTaskRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'UpdateTaskRequest', 'id'),
          stateCommaOmitempty: stateCommaOmitempty,
          nameCommaOmitempty: nameCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
