// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'create_plan_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CreatePlanRequest extends CreatePlanRequest {
  @override
  final String title;
  @override
  final String? description;
  @override
  final String? projectId;
  @override
  final String? projectPath;
  @override
  final String sessionId;

  factory _$CreatePlanRequest(
          [void Function(CreatePlanRequestBuilder)? updates]) =>
      (CreatePlanRequestBuilder()..update(updates))._build();

  _$CreatePlanRequest._(
      {required this.title,
      this.description,
      this.projectId,
      this.projectPath,
      required this.sessionId})
      : super._();
  @override
  CreatePlanRequest rebuild(void Function(CreatePlanRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CreatePlanRequestBuilder toBuilder() =>
      CreatePlanRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CreatePlanRequest &&
        title == other.title &&
        description == other.description &&
        projectId == other.projectId &&
        projectPath == other.projectPath &&
        sessionId == other.sessionId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, title.hashCode);
    _$hash = $jc(_$hash, description.hashCode);
    _$hash = $jc(_$hash, projectId.hashCode);
    _$hash = $jc(_$hash, projectPath.hashCode);
    _$hash = $jc(_$hash, sessionId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CreatePlanRequest')
          ..add('title', title)
          ..add('description', description)
          ..add('projectId', projectId)
          ..add('projectPath', projectPath)
          ..add('sessionId', sessionId))
        .toString();
  }
}

class CreatePlanRequestBuilder
    implements Builder<CreatePlanRequest, CreatePlanRequestBuilder> {
  _$CreatePlanRequest? _$v;

  String? _title;
  String? get title => _$this._title;
  set title(String? title) => _$this._title = title;

  String? _description;
  String? get description => _$this._description;
  set description(String? description) =>
      _$this._description = description;

  String? _projectId;
  String? get projectId => _$this._projectId;
  set projectId(String? projectId) =>
      _$this._projectId = projectId;

  String? _projectPath;
  String? get projectPath => _$this._projectPath;
  set projectPath(String? projectPath) =>
      _$this._projectPath = projectPath;

  String? _sessionId;
  String? get sessionId => _$this._sessionId;
  set sessionId(String? sessionId) => _$this._sessionId = sessionId;

  CreatePlanRequestBuilder() {
    CreatePlanRequest._defaults(this);
  }

  CreatePlanRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _title = $v.title;
      _description = $v.description;
      _projectId = $v.projectId;
      _projectPath = $v.projectPath;
      _sessionId = $v.sessionId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CreatePlanRequest other) {
    _$v = other as _$CreatePlanRequest;
  }

  @override
  void update(void Function(CreatePlanRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CreatePlanRequest build() => _build();

  _$CreatePlanRequest _build() {
    final _$result = _$v ??
        _$CreatePlanRequest._(
          title: BuiltValueNullFieldError.checkNotNull(
              title, r'CreatePlanRequest', 'title'),
          description: description,
          projectId: projectId,
          projectPath: projectPath,
          sessionId: BuiltValueNullFieldError.checkNotNull(
              sessionId, r'CreatePlanRequest', 'sessionId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
