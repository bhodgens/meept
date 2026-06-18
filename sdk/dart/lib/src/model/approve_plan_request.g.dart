// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'approve_plan_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ApprovePlanRequest extends ApprovePlanRequest {
  @override
  final String planId;
  @override
  final String sessionId;
  @override
  final String by;

  factory _$ApprovePlanRequest(
          [void Function(ApprovePlanRequestBuilder)? updates]) =>
      (ApprovePlanRequestBuilder()..update(updates))._build();

  _$ApprovePlanRequest._(
      {required this.planId, required this.sessionId, required this.by})
      : super._();
  @override
  ApprovePlanRequest rebuild(
          void Function(ApprovePlanRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ApprovePlanRequestBuilder toBuilder() =>
      ApprovePlanRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ApprovePlanRequest &&
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
    return (newBuiltValueToStringHelper(r'ApprovePlanRequest')
          ..add('planId', planId)
          ..add('sessionId', sessionId)
          ..add('by', by))
        .toString();
  }
}

class ApprovePlanRequestBuilder
    implements Builder<ApprovePlanRequest, ApprovePlanRequestBuilder> {
  _$ApprovePlanRequest? _$v;

  String? _planId;
  String? get planId => _$this._planId;
  set planId(String? planId) => _$this._planId = planId;

  String? _sessionId;
  String? get sessionId => _$this._sessionId;
  set sessionId(String? sessionId) => _$this._sessionId = sessionId;

  String? _by;
  String? get by => _$this._by;
  set by(String? by) => _$this._by = by;

  ApprovePlanRequestBuilder() {
    ApprovePlanRequest._defaults(this);
  }

  ApprovePlanRequestBuilder get _$this {
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
  void replace(ApprovePlanRequest other) {
    _$v = other as _$ApprovePlanRequest;
  }

  @override
  void update(void Function(ApprovePlanRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ApprovePlanRequest build() => _build();

  _$ApprovePlanRequest _build() {
    final _$result = _$v ??
        _$ApprovePlanRequest._(
          planId: BuiltValueNullFieldError.checkNotNull(
              planId, r'ApprovePlanRequest', 'planId'),
          sessionId: BuiltValueNullFieldError.checkNotNull(
              sessionId, r'ApprovePlanRequest', 'sessionId'),
          by: BuiltValueNullFieldError.checkNotNull(
              by, r'ApprovePlanRequest', 'by'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
