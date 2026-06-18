// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'set_project_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SetProjectRequest extends SetProjectRequest {
  @override
  final String sessionId;
  @override
  final String projectId;

  factory _$SetProjectRequest(
          [void Function(SetProjectRequestBuilder)? updates]) =>
      (SetProjectRequestBuilder()..update(updates))._build();

  _$SetProjectRequest._({required this.sessionId, required this.projectId})
      : super._();
  @override
  SetProjectRequest rebuild(void Function(SetProjectRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SetProjectRequestBuilder toBuilder() =>
      SetProjectRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SetProjectRequest &&
        sessionId == other.sessionId &&
        projectId == other.projectId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, sessionId.hashCode);
    _$hash = $jc(_$hash, projectId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SetProjectRequest')
          ..add('sessionId', sessionId)
          ..add('projectId', projectId))
        .toString();
  }
}

class SetProjectRequestBuilder
    implements Builder<SetProjectRequest, SetProjectRequestBuilder> {
  _$SetProjectRequest? _$v;

  String? _sessionId;
  String? get sessionId => _$this._sessionId;
  set sessionId(String? sessionId) => _$this._sessionId = sessionId;

  String? _projectId;
  String? get projectId => _$this._projectId;
  set projectId(String? projectId) => _$this._projectId = projectId;

  SetProjectRequestBuilder() {
    SetProjectRequest._defaults(this);
  }

  SetProjectRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _sessionId = $v.sessionId;
      _projectId = $v.projectId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SetProjectRequest other) {
    _$v = other as _$SetProjectRequest;
  }

  @override
  void update(void Function(SetProjectRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SetProjectRequest build() => _build();

  _$SetProjectRequest _build() {
    final _$result = _$v ??
        _$SetProjectRequest._(
          sessionId: BuiltValueNullFieldError.checkNotNull(
              sessionId, r'SetProjectRequest', 'sessionId'),
          projectId: BuiltValueNullFieldError.checkNotNull(
              projectId, r'SetProjectRequest', 'projectId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
