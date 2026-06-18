// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'validate_improvement_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ValidateImprovementRequest extends ValidateImprovementRequest {
  @override
  final String improvementId;

  factory _$ValidateImprovementRequest(
          [void Function(ValidateImprovementRequestBuilder)? updates]) =>
      (ValidateImprovementRequestBuilder()..update(updates))._build();

  _$ValidateImprovementRequest._({required this.improvementId}) : super._();
  @override
  ValidateImprovementRequest rebuild(
          void Function(ValidateImprovementRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ValidateImprovementRequestBuilder toBuilder() =>
      ValidateImprovementRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ValidateImprovementRequest &&
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
    return (newBuiltValueToStringHelper(r'ValidateImprovementRequest')
          ..add('improvementId', improvementId))
        .toString();
  }
}

class ValidateImprovementRequestBuilder
    implements
        Builder<ValidateImprovementRequest, ValidateImprovementRequestBuilder> {
  _$ValidateImprovementRequest? _$v;

  String? _improvementId;
  String? get improvementId => _$this._improvementId;
  set improvementId(String? improvementId) =>
      _$this._improvementId = improvementId;

  ValidateImprovementRequestBuilder() {
    ValidateImprovementRequest._defaults(this);
  }

  ValidateImprovementRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _improvementId = $v.improvementId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ValidateImprovementRequest other) {
    _$v = other as _$ValidateImprovementRequest;
  }

  @override
  void update(void Function(ValidateImprovementRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ValidateImprovementRequest build() => _build();

  _$ValidateImprovementRequest _build() {
    final _$result = _$v ??
        _$ValidateImprovementRequest._(
          improvementId: BuiltValueNullFieldError.checkNotNull(
              improvementId, r'ValidateImprovementRequest', 'improvementId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
