// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'generate_improvement_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$GenerateImprovementRequest extends GenerateImprovementRequest {
  @override
  final String improvementId;

  factory _$GenerateImprovementRequest(
          [void Function(GenerateImprovementRequestBuilder)? updates]) =>
      (GenerateImprovementRequestBuilder()..update(updates))._build();

  _$GenerateImprovementRequest._({required this.improvementId}) : super._();
  @override
  GenerateImprovementRequest rebuild(
          void Function(GenerateImprovementRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  GenerateImprovementRequestBuilder toBuilder() =>
      GenerateImprovementRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is GenerateImprovementRequest &&
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
    return (newBuiltValueToStringHelper(r'GenerateImprovementRequest')
          ..add('improvementId', improvementId))
        .toString();
  }
}

class GenerateImprovementRequestBuilder
    implements
        Builder<GenerateImprovementRequest, GenerateImprovementRequestBuilder> {
  _$GenerateImprovementRequest? _$v;

  String? _improvementId;
  String? get improvementId => _$this._improvementId;
  set improvementId(String? improvementId) =>
      _$this._improvementId = improvementId;

  GenerateImprovementRequestBuilder() {
    GenerateImprovementRequest._defaults(this);
  }

  GenerateImprovementRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _improvementId = $v.improvementId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(GenerateImprovementRequest other) {
    _$v = other as _$GenerateImprovementRequest;
  }

  @override
  void update(void Function(GenerateImprovementRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  GenerateImprovementRequest build() => _build();

  _$GenerateImprovementRequest _build() {
    final _$result = _$v ??
        _$GenerateImprovementRequest._(
          improvementId: BuiltValueNullFieldError.checkNotNull(
              improvementId, r'GenerateImprovementRequest', 'improvementId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
