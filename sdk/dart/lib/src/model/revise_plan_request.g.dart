// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'revise_plan_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$RevisePlanRequest extends RevisePlanRequest {
  @override
  final String planId;
  @override
  final String sessionId;
  @override
  final String feedback;

  factory _$RevisePlanRequest(
          [void Function(RevisePlanRequestBuilder)? updates]) =>
      (RevisePlanRequestBuilder()..update(updates))._build();

  _$RevisePlanRequest._(
      {required this.planId, required this.sessionId, required this.feedback})
      : super._();
  @override
  RevisePlanRequest rebuild(void Function(RevisePlanRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  RevisePlanRequestBuilder toBuilder() =>
      RevisePlanRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is RevisePlanRequest &&
        planId == other.planId &&
        sessionId == other.sessionId &&
        feedback == other.feedback;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, planId.hashCode);
    _$hash = $jc(_$hash, sessionId.hashCode);
    _$hash = $jc(_$hash, feedback.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'RevisePlanRequest')
          ..add('planId', planId)
          ..add('sessionId', sessionId)
          ..add('feedback', feedback))
        .toString();
  }
}

class RevisePlanRequestBuilder
    implements Builder<RevisePlanRequest, RevisePlanRequestBuilder> {
  _$RevisePlanRequest? _$v;

  String? _planId;
  String? get planId => _$this._planId;
  set planId(String? planId) => _$this._planId = planId;

  String? _sessionId;
  String? get sessionId => _$this._sessionId;
  set sessionId(String? sessionId) => _$this._sessionId = sessionId;

  String? _feedback;
  String? get feedback => _$this._feedback;
  set feedback(String? feedback) => _$this._feedback = feedback;

  RevisePlanRequestBuilder() {
    RevisePlanRequest._defaults(this);
  }

  RevisePlanRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _planId = $v.planId;
      _sessionId = $v.sessionId;
      _feedback = $v.feedback;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(RevisePlanRequest other) {
    _$v = other as _$RevisePlanRequest;
  }

  @override
  void update(void Function(RevisePlanRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  RevisePlanRequest build() => _build();

  _$RevisePlanRequest _build() {
    final _$result = _$v ??
        _$RevisePlanRequest._(
          planId: BuiltValueNullFieldError.checkNotNull(
              planId, r'RevisePlanRequest', 'planId'),
          sessionId: BuiltValueNullFieldError.checkNotNull(
              sessionId, r'RevisePlanRequest', 'sessionId'),
          feedback: BuiltValueNullFieldError.checkNotNull(
              feedback, r'RevisePlanRequest', 'feedback'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
