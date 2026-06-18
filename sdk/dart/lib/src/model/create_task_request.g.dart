// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'create_task_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CreateTaskRequest extends CreateTaskRequest {
  @override
  final String name;
  @override
  final String? descriptionCommaOmitempty;
  @override
  final String? sessionIdCommaOmitempty;

  factory _$CreateTaskRequest(
          [void Function(CreateTaskRequestBuilder)? updates]) =>
      (CreateTaskRequestBuilder()..update(updates))._build();

  _$CreateTaskRequest._(
      {required this.name,
      this.descriptionCommaOmitempty,
      this.sessionIdCommaOmitempty})
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
        descriptionCommaOmitempty == other.descriptionCommaOmitempty &&
        sessionIdCommaOmitempty == other.sessionIdCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, descriptionCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, sessionIdCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CreateTaskRequest')
          ..add('name', name)
          ..add('descriptionCommaOmitempty', descriptionCommaOmitempty)
          ..add('sessionIdCommaOmitempty', sessionIdCommaOmitempty))
        .toString();
  }
}

class CreateTaskRequestBuilder
    implements Builder<CreateTaskRequest, CreateTaskRequestBuilder> {
  _$CreateTaskRequest? _$v;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _descriptionCommaOmitempty;
  String? get descriptionCommaOmitempty => _$this._descriptionCommaOmitempty;
  set descriptionCommaOmitempty(String? descriptionCommaOmitempty) =>
      _$this._descriptionCommaOmitempty = descriptionCommaOmitempty;

  String? _sessionIdCommaOmitempty;
  String? get sessionIdCommaOmitempty => _$this._sessionIdCommaOmitempty;
  set sessionIdCommaOmitempty(String? sessionIdCommaOmitempty) =>
      _$this._sessionIdCommaOmitempty = sessionIdCommaOmitempty;

  CreateTaskRequestBuilder() {
    CreateTaskRequest._defaults(this);
  }

  CreateTaskRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _name = $v.name;
      _descriptionCommaOmitempty = $v.descriptionCommaOmitempty;
      _sessionIdCommaOmitempty = $v.sessionIdCommaOmitempty;
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
          descriptionCommaOmitempty: descriptionCommaOmitempty,
          sessionIdCommaOmitempty: sessionIdCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
