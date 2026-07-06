// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'update_task_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$UpdateTaskRequest extends UpdateTaskRequest {
  @override
  final String id;
  @override
  final String? state;
  @override
  final String? name;

  factory _$UpdateTaskRequest(
          [void Function(UpdateTaskRequestBuilder)? updates]) =>
      (UpdateTaskRequestBuilder()..update(updates))._build();

  _$UpdateTaskRequest._(
      {required this.id, this.state, this.name})
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
        state == other.state &&
        name == other.name;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, state.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'UpdateTaskRequest')
          ..add('id', id)
          ..add('state', state)
          ..add('name', name))
        .toString();
  }
}

class UpdateTaskRequestBuilder
    implements Builder<UpdateTaskRequest, UpdateTaskRequestBuilder> {
  _$UpdateTaskRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _state;
  String? get state => _$this._state;
  set state(String? state) =>
      _$this._state = state;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) =>
      _$this._name = name;

  UpdateTaskRequestBuilder() {
    UpdateTaskRequest._defaults(this);
  }

  UpdateTaskRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _state = $v.state;
      _name = $v.name;
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
          state: state,
          name: name,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
