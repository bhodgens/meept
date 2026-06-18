// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'reject_improvement_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$RejectImprovementRequest extends RejectImprovementRequest {
  @override
  final String improvementId;
  @override
  final String reason;

  factory _$RejectImprovementRequest(
          [void Function(RejectImprovementRequestBuilder)? updates]) =>
      (RejectImprovementRequestBuilder()..update(updates))._build();

  _$RejectImprovementRequest._(
      {required this.improvementId, required this.reason})
      : super._();
  @override
  RejectImprovementRequest rebuild(
          void Function(RejectImprovementRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  RejectImprovementRequestBuilder toBuilder() =>
      RejectImprovementRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is RejectImprovementRequest &&
        improvementId == other.improvementId &&
        reason == other.reason;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, improvementId.hashCode);
    _$hash = $jc(_$hash, reason.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'RejectImprovementRequest')
          ..add('improvementId', improvementId)
          ..add('reason', reason))
        .toString();
  }
}

class RejectImprovementRequestBuilder
    implements
        Builder<RejectImprovementRequest, RejectImprovementRequestBuilder> {
  _$RejectImprovementRequest? _$v;

  String? _improvementId;
  String? get improvementId => _$this._improvementId;
  set improvementId(String? improvementId) =>
      _$this._improvementId = improvementId;

  String? _reason;
  String? get reason => _$this._reason;
  set reason(String? reason) => _$this._reason = reason;

  RejectImprovementRequestBuilder() {
    RejectImprovementRequest._defaults(this);
  }

  RejectImprovementRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _improvementId = $v.improvementId;
      _reason = $v.reason;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(RejectImprovementRequest other) {
    _$v = other as _$RejectImprovementRequest;
  }

  @override
  void update(void Function(RejectImprovementRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  RejectImprovementRequest build() => _build();

  _$RejectImprovementRequest _build() {
    final _$result = _$v ??
        _$RejectImprovementRequest._(
          improvementId: BuiltValueNullFieldError.checkNotNull(
              improvementId, r'RejectImprovementRequest', 'improvementId'),
          reason: BuiltValueNullFieldError.checkNotNull(
              reason, r'RejectImprovementRequest', 'reason'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
