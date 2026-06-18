// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'register_project_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$RegisterProjectRequest extends RegisterProjectRequest {
  @override
  final String? idCommaOmitempty;
  @override
  final String name;
  @override
  final String? gitUrlCommaOmitempty;
  @override
  final String? localPathCommaOmitempty;

  factory _$RegisterProjectRequest(
          [void Function(RegisterProjectRequestBuilder)? updates]) =>
      (RegisterProjectRequestBuilder()..update(updates))._build();

  _$RegisterProjectRequest._(
      {this.idCommaOmitempty,
      required this.name,
      this.gitUrlCommaOmitempty,
      this.localPathCommaOmitempty})
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
        idCommaOmitempty == other.idCommaOmitempty &&
        name == other.name &&
        gitUrlCommaOmitempty == other.gitUrlCommaOmitempty &&
        localPathCommaOmitempty == other.localPathCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, idCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, gitUrlCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, localPathCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'RegisterProjectRequest')
          ..add('idCommaOmitempty', idCommaOmitempty)
          ..add('name', name)
          ..add('gitUrlCommaOmitempty', gitUrlCommaOmitempty)
          ..add('localPathCommaOmitempty', localPathCommaOmitempty))
        .toString();
  }
}

class RegisterProjectRequestBuilder
    implements Builder<RegisterProjectRequest, RegisterProjectRequestBuilder> {
  _$RegisterProjectRequest? _$v;

  String? _idCommaOmitempty;
  String? get idCommaOmitempty => _$this._idCommaOmitempty;
  set idCommaOmitempty(String? idCommaOmitempty) =>
      _$this._idCommaOmitempty = idCommaOmitempty;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _gitUrlCommaOmitempty;
  String? get gitUrlCommaOmitempty => _$this._gitUrlCommaOmitempty;
  set gitUrlCommaOmitempty(String? gitUrlCommaOmitempty) =>
      _$this._gitUrlCommaOmitempty = gitUrlCommaOmitempty;

  String? _localPathCommaOmitempty;
  String? get localPathCommaOmitempty => _$this._localPathCommaOmitempty;
  set localPathCommaOmitempty(String? localPathCommaOmitempty) =>
      _$this._localPathCommaOmitempty = localPathCommaOmitempty;

  RegisterProjectRequestBuilder() {
    RegisterProjectRequest._defaults(this);
  }

  RegisterProjectRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _idCommaOmitempty = $v.idCommaOmitempty;
      _name = $v.name;
      _gitUrlCommaOmitempty = $v.gitUrlCommaOmitempty;
      _localPathCommaOmitempty = $v.localPathCommaOmitempty;
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
          idCommaOmitempty: idCommaOmitempty,
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'RegisterProjectRequest', 'name'),
          gitUrlCommaOmitempty: gitUrlCommaOmitempty,
          localPathCommaOmitempty: localPathCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
