// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'register_project_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$RegisterProjectRequest extends RegisterProjectRequest {
  @override
  final String? id;
  @override
  final String name;
  @override
  final String? gitUrl;
  @override
  final String? localPath;

  factory _$RegisterProjectRequest(
          [void Function(RegisterProjectRequestBuilder)? updates]) =>
      (RegisterProjectRequestBuilder()..update(updates))._build();

  _$RegisterProjectRequest._(
      {this.id,
      required this.name,
      this.gitUrl,
      this.localPath})
      : super._();
  @override
  RegisterProjectRequest rebuild(
          void Function(RegisterProjectRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  RegisterProjectRequestBuilder toBuilder() =>
      RegisterProjectRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is RegisterProjectRequest &&
        id == other.id &&
        name == other.name &&
        gitUrl == other.gitUrl &&
        localPath == other.localPath;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, gitUrl.hashCode);
    _$hash = $jc(_$hash, localPath.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'RegisterProjectRequest')
          ..add('id', id)
          ..add('name', name)
          ..add('gitUrl', gitUrl)
          ..add('localPath', localPath))
        .toString();
  }
}

class RegisterProjectRequestBuilder
    implements Builder<RegisterProjectRequest, RegisterProjectRequestBuilder> {
  _$RegisterProjectRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) =>
      _$this._id = id;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _gitUrl;
  String? get gitUrl => _$this._gitUrl;
  set gitUrl(String? gitUrl) =>
      _$this._gitUrl = gitUrl;

  String? _localPath;
  String? get localPath => _$this._localPath;
  set localPath(String? localPath) =>
      _$this._localPath = localPath;

  RegisterProjectRequestBuilder() {
    RegisterProjectRequest._defaults(this);
  }

  RegisterProjectRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _name = $v.name;
      _gitUrl = $v.gitUrl;
      _localPath = $v.localPath;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(RegisterProjectRequest other) {
    _$v = other as _$RegisterProjectRequest;
  }

  @override
  void update(void Function(RegisterProjectRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  RegisterProjectRequest build() => _build();

  _$RegisterProjectRequest _build() {
    final _$result = _$v ??
        _$RegisterProjectRequest._(
          id: id,
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'RegisterProjectRequest', 'name'),
          gitUrl: gitUrl,
          localPath: localPath,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
