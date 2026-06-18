// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'confirm_plan_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ConfirmPlanRequest extends ConfirmPlanRequest {
  @override
  final String planId;
  @override
  final String sessionId;
  @override
  final String by;

  factory _$ConfirmPlanRequest(
          [void Function(ConfirmPlanRequestBuilder)? updates]) =>
      (ConfirmPlanRequestBuilder()..update(updates))._build();

  _$ConfirmPlanRequest._(
      {required this.planId, required this.sessionId, required this.by})
      : super._();
  @override
  ConfirmPlanRequest rebuild(
          void Function(ConfirmPlanRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ConfirmPlanRequestBuilder toBuilder() =>
      ConfirmPlanRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ConfirmPlanRequest &&
        planId == other.planId &&
        sessionId == other.sessionId &&
        by == other.by;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, planId.hashCode);
    _$hash = $jc(_$hash, sessionId.hashCode);
    _$hash = $jc(_$hash, by.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ConfirmPlanRequest')
          ..add('planId', planId)
          ..add('sessionId', sessionId)
          ..add('by', by))
        .toString();
  }
}

class ConfirmPlanRequestBuilder
    implements Builder<ConfirmPlanRequest, ConfirmPlanRequestBuilder> {
  _$ConfirmPlanRequest? _$v;

  String? _planId;
  String? get planId => _$this._planId;
  set planId(String? planId) => _$this._planId = planId;

  String? _sessionId;
  String? get sessionId => _$this._sessionId;
  set sessionId(String? sessionId) => _$this._sessionId = sessionId;

  String? _by;
  String? get by => _$this._by;
  set by(String? by) => _$this._by = by;

  ConfirmPlanRequestBuilder() {
    ConfirmPlanRequest._defaults(this);
  }

  ConfirmPlanRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _planId = $v.planId;
      _sessionId = $v.sessionId;
      _by = $v.by;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ConfirmPlanRequest other) {
    _$v = other as _$ConfirmPlanRequest;
  }

  @override
  void update(void Function(ConfirmPlanRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ConfirmPlanRequest build() => _build();

  _$ConfirmPlanRequest _build() {
    final _$result = _$v ??
        _$ConfirmPlanRequest._(
          planId: BuiltValueNullFieldError.checkNotNull(
              planId, r'ConfirmPlanRequest', 'planId'),
          sessionId: BuiltValueNullFieldError.checkNotNull(
              sessionId, r'ConfirmPlanRequest', 'sessionId'),
          by: BuiltValueNullFieldError.checkNotNull(
              by, r'ConfirmPlanRequest', 'by'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
