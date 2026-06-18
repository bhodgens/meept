// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'reject_plan_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$RejectPlanRequest extends RejectPlanRequest {
  @override
  final String planId;
  @override
  final String sessionId;
  @override
  final String by;
  @override
  final String? reasonCommaOmitempty;

  factory _$RejectPlanRequest(
          [void Function(RejectPlanRequestBuilder)? updates]) =>
      (RejectPlanRequestBuilder()..update(updates))._build();

  _$RejectPlanRequest._(
      {required this.planId,
      required this.sessionId,
      required this.by,
      this.reasonCommaOmitempty})
      : super._();
  @override
  RejectPlanRequest rebuild(void Function(RejectPlanRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  RejectPlanRequestBuilder toBuilder() =>
      RejectPlanRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is RejectPlanRequest &&
        planId == other.planId &&
        sessionId == other.sessionId &&
        by == other.by &&
        reasonCommaOmitempty == other.reasonCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, planId.hashCode);
    _$hash = $jc(_$hash, sessionId.hashCode);
    _$hash = $jc(_$hash, by.hashCode);
    _$hash = $jc(_$hash, reasonCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'RejectPlanRequest')
          ..add('planId', planId)
          ..add('sessionId', sessionId)
          ..add('by', by)
          ..add('reasonCommaOmitempty', reasonCommaOmitempty))
        .toString();
  }
}

class RejectPlanRequestBuilder
    implements Builder<RejectPlanRequest, RejectPlanRequestBuilder> {
  _$RejectPlanRequest? _$v;

  String? _planId;
  String? get planId => _$this._planId;
  set planId(String? planId) => _$this._planId = planId;

  String? _sessionId;
  String? get sessionId => _$this._sessionId;
  set sessionId(String? sessionId) => _$this._sessionId = sessionId;

  String? _by;
  String? get by => _$this._by;
  set by(String? by) => _$this._by = by;

  String? _reasonCommaOmitempty;
  String? get reasonCommaOmitempty => _$this._reasonCommaOmitempty;
  set reasonCommaOmitempty(String? reasonCommaOmitempty) =>
      _$this._reasonCommaOmitempty = reasonCommaOmitempty;

  RejectPlanRequestBuilder() {
    RejectPlanRequest._defaults(this);
  }

  RejectPlanRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _planId = $v.planId;
      _sessionId = $v.sessionId;
      _by = $v.by;
      _reasonCommaOmitempty = $v.reasonCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(RejectPlanRequest other) {
    _$v = other as _$RejectPlanRequest;
  }

  @override
  void update(void Function(RejectPlanRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  RejectPlanRequest build() => _build();

  _$RejectPlanRequest _build() {
    final _$result = _$v ??
        _$RejectPlanRequest._(
          planId: BuiltValueNullFieldError.checkNotNull(
              planId, r'RejectPlanRequest', 'planId'),
          sessionId: BuiltValueNullFieldError.checkNotNull(
              sessionId, r'RejectPlanRequest', 'sessionId'),
          by: BuiltValueNullFieldError.checkNotNull(
              by, r'RejectPlanRequest', 'by'),
          reasonCommaOmitempty: reasonCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
