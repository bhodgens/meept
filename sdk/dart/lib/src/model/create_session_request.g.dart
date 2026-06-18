// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'create_session_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CreateSessionRequest extends CreateSessionRequest {
  @override
  final String? nameCommaOmitempty;

  factory _$CreateSessionRequest(
          [void Function(CreateSessionRequestBuilder)? updates]) =>
      (CreateSessionRequestBuilder()..update(updates))._build();

  _$CreateSessionRequest._({this.nameCommaOmitempty}) : super._();
  @override
  CreateSessionRequest rebuild(
          void Function(CreateSessionRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CreateSessionRequestBuilder toBuilder() =>
      CreateSessionRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CreateSessionRequest &&
        nameCommaOmitempty == other.nameCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, nameCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CreateSessionRequest')
          ..add('nameCommaOmitempty', nameCommaOmitempty))
        .toString();
  }
}

class CreateSessionRequestBuilder
    implements Builder<CreateSessionRequest, CreateSessionRequestBuilder> {
  _$CreateSessionRequest? _$v;

  String? _nameCommaOmitempty;
  String? get nameCommaOmitempty => _$this._nameCommaOmitempty;
  set nameCommaOmitempty(String? nameCommaOmitempty) =>
      _$this._nameCommaOmitempty = nameCommaOmitempty;

  CreateSessionRequestBuilder() {
    CreateSessionRequest._defaults(this);
  }

  CreateSessionRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _nameCommaOmitempty = $v.nameCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CreateSessionRequest other) {
    _$v = other as _$CreateSessionRequest;
  }

  @override
  void update(void Function(CreateSessionRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CreateSessionRequest build() => _build();

  _$CreateSessionRequest _build() {
    final _$result = _$v ??
        _$CreateSessionRequest._(
          nameCommaOmitempty: nameCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
