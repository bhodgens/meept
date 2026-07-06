// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'create_task_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CreateTaskRequest extends CreateTaskRequest {
  @override
  final String name;
  @override
  final String? description;
  @override
  final String? sessionId;

  factory _$CreateTaskRequest(
          [void Function(CreateTaskRequestBuilder)? updates]) =>
      (CreateTaskRequestBuilder()..update(updates))._build();

  _$CreateTaskRequest._(
      {required this.name,
      this.description,
      this.sessionId})
      : super._();
  @override
  CreateTaskRequest rebuild(void Function(CreateTaskRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CreateTaskRequestBuilder toBuilder() =>
      CreateTaskRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CreateTaskRequest &&
        name == other.name &&
        description == other.description &&
        sessionId == other.sessionId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, description.hashCode);
    _$hash = $jc(_$hash, sessionId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CreateTaskRequest')
          ..add('name', name)
          ..add('description', description)
          ..add('sessionId', sessionId))
        .toString();
  }
}

class CreateTaskRequestBuilder
    implements Builder<CreateTaskRequest, CreateTaskRequestBuilder> {
  _$CreateTaskRequest? _$v;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _description;
  String? get description => _$this._description;
  set description(String? description) =>
      _$this._description = description;

  String? _sessionId;
  String? get sessionId => _$this._sessionId;
  set sessionId(String? sessionId) =>
      _$this._sessionId = sessionId;

  CreateTaskRequestBuilder() {
    CreateTaskRequest._defaults(this);
  }

  CreateTaskRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _name = $v.name;
      _description = $v.description;
      _sessionId = $v.sessionId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CreateTaskRequest other) {
    _$v = other as _$CreateTaskRequest;
  }

  @override
  void update(void Function(CreateTaskRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CreateTaskRequest build() => _build();

  _$CreateTaskRequest _build() {
    final _$result = _$v ??
        _$CreateTaskRequest._(
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'CreateTaskRequest', 'name'),
          description: description,
          sessionId: sessionId,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
