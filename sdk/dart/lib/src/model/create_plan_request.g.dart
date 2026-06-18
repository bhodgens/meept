// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'create_plan_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CreatePlanRequest extends CreatePlanRequest {
  @override
  final String title;
  @override
  final String? descriptionCommaOmitempty;
  @override
  final String? projectIdCommaOmitempty;
  @override
  final String? projectPathCommaOmitempty;
  @override
  final String sessionId;

  factory _$CreatePlanRequest(
          [void Function(CreatePlanRequestBuilder)? updates]) =>
      (CreatePlanRequestBuilder()..update(updates))._build();

  _$CreatePlanRequest._(
      {required this.title,
      this.descriptionCommaOmitempty,
      this.projectIdCommaOmitempty,
      this.projectPathCommaOmitempty,
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
        descriptionCommaOmitempty == other.descriptionCommaOmitempty &&
        projectIdCommaOmitempty == other.projectIdCommaOmitempty &&
        projectPathCommaOmitempty == other.projectPathCommaOmitempty &&
        sessionId == other.sessionId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, title.hashCode);
    _$hash = $jc(_$hash, descriptionCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, projectIdCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, projectPathCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, sessionId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CreatePlanRequest')
          ..add('title', title)
          ..add('descriptionCommaOmitempty', descriptionCommaOmitempty)
          ..add('projectIdCommaOmitempty', projectIdCommaOmitempty)
          ..add('projectPathCommaOmitempty', projectPathCommaOmitempty)
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

  String? _descriptionCommaOmitempty;
  String? get descriptionCommaOmitempty => _$this._descriptionCommaOmitempty;
  set descriptionCommaOmitempty(String? descriptionCommaOmitempty) =>
      _$this._descriptionCommaOmitempty = descriptionCommaOmitempty;

  String? _projectIdCommaOmitempty;
  String? get projectIdCommaOmitempty => _$this._projectIdCommaOmitempty;
  set projectIdCommaOmitempty(String? projectIdCommaOmitempty) =>
      _$this._projectIdCommaOmitempty = projectIdCommaOmitempty;

  String? _projectPathCommaOmitempty;
  String? get projectPathCommaOmitempty => _$this._projectPathCommaOmitempty;
  set projectPathCommaOmitempty(String? projectPathCommaOmitempty) =>
      _$this._projectPathCommaOmitempty = projectPathCommaOmitempty;

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
      _descriptionCommaOmitempty = $v.descriptionCommaOmitempty;
      _projectIdCommaOmitempty = $v.projectIdCommaOmitempty;
      _projectPathCommaOmitempty = $v.projectPathCommaOmitempty;
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
          descriptionCommaOmitempty: descriptionCommaOmitempty,
          projectIdCommaOmitempty: projectIdCommaOmitempty,
          projectPathCommaOmitempty: projectPathCommaOmitempty,
          sessionId: BuiltValueNullFieldError.checkNotNull(
              sessionId, r'CreatePlanRequest', 'sessionId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
