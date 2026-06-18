// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'complete_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CompleteRequest extends CompleteRequest {
  @override
  final String jobId;
  @override
  final JsonObject? resultCommaOmitempty;

  factory _$CompleteRequest([void Function(CompleteRequestBuilder)? updates]) =>
      (CompleteRequestBuilder()..update(updates))._build();

  _$CompleteRequest._({required this.jobId, this.resultCommaOmitempty})
      : super._();
  @override
  CompleteRequest rebuild(void Function(CompleteRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CompleteRequestBuilder toBuilder() => CompleteRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CompleteRequest &&
        jobId == other.jobId &&
        resultCommaOmitempty == other.resultCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, jobId.hashCode);
    _$hash = $jc(_$hash, resultCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CompleteRequest')
          ..add('jobId', jobId)
          ..add('resultCommaOmitempty', resultCommaOmitempty))
        .toString();
  }
}

class CompleteRequestBuilder
    implements Builder<CompleteRequest, CompleteRequestBuilder> {
  _$CompleteRequest? _$v;

  String? _jobId;
  String? get jobId => _$this._jobId;
  set jobId(String? jobId) => _$this._jobId = jobId;

  JsonObject? _resultCommaOmitempty;
  JsonObject? get resultCommaOmitempty => _$this._resultCommaOmitempty;
  set resultCommaOmitempty(JsonObject? resultCommaOmitempty) =>
      _$this._resultCommaOmitempty = resultCommaOmitempty;

  CompleteRequestBuilder() {
    CompleteRequest._defaults(this);
  }

  CompleteRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _jobId = $v.jobId;
      _resultCommaOmitempty = $v.resultCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CompleteRequest other) {
    _$v = other as _$CompleteRequest;
  }

  @override
  void update(void Function(CompleteRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CompleteRequest build() => _build();

  _$CompleteRequest _build() {
    final _$result = _$v ??
        _$CompleteRequest._(
          jobId: BuiltValueNullFieldError.checkNotNull(
              jobId, r'CompleteRequest', 'jobId'),
          resultCommaOmitempty: resultCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
