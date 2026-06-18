// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'apply_improvement_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ApplyImprovementRequest extends ApplyImprovementRequest {
  @override
  final String improvementId;

  factory _$ApplyImprovementRequest(
          [void Function(ApplyImprovementRequestBuilder)? updates]) =>
      (ApplyImprovementRequestBuilder()..update(updates))._build();

  _$ApplyImprovementRequest._({required this.improvementId}) : super._();
  @override
  ApplyImprovementRequest rebuild(
          void Function(ApplyImprovementRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ApplyImprovementRequestBuilder toBuilder() =>
      ApplyImprovementRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ApplyImprovementRequest &&
        improvementId == other.improvementId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, improvementId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ApplyImprovementRequest')
          ..add('improvementId', improvementId))
        .toString();
  }
}

class ApplyImprovementRequestBuilder
    implements
        Builder<ApplyImprovementRequest, ApplyImprovementRequestBuilder> {
  _$ApplyImprovementRequest? _$v;

  String? _improvementId;
  String? get improvementId => _$this._improvementId;
  set improvementId(String? improvementId) =>
      _$this._improvementId = improvementId;

  ApplyImprovementRequestBuilder() {
    ApplyImprovementRequest._defaults(this);
  }

  ApplyImprovementRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _improvementId = $v.improvementId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ApplyImprovementRequest other) {
    _$v = other as _$ApplyImprovementRequest;
  }

  @override
  void update(void Function(ApplyImprovementRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ApplyImprovementRequest build() => _build();

  _$ApplyImprovementRequest _build() {
    final _$result = _$v ??
        _$ApplyImprovementRequest._(
          improvementId: BuiltValueNullFieldError.checkNotNull(
              improvementId, r'ApplyImprovementRequest', 'improvementId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
