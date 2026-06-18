// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'retry_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$RetryRequest extends RetryRequest {
  @override
  final String jobId;

  factory _$RetryRequest([void Function(RetryRequestBuilder)? updates]) =>
      (RetryRequestBuilder()..update(updates))._build();

  _$RetryRequest._({required this.jobId}) : super._();
  @override
  RetryRequest rebuild(void Function(RetryRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  RetryRequestBuilder toBuilder() => RetryRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is RetryRequest && jobId == other.jobId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, jobId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'RetryRequest')..add('jobId', jobId))
        .toString();
  }
}

class RetryRequestBuilder
    implements Builder<RetryRequest, RetryRequestBuilder> {
  _$RetryRequest? _$v;

  String? _jobId;
  String? get jobId => _$this._jobId;
  set jobId(String? jobId) => _$this._jobId = jobId;

  RetryRequestBuilder() {
    RetryRequest._defaults(this);
  }

  RetryRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _jobId = $v.jobId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(RetryRequest other) {
    _$v = other as _$RetryRequest;
  }

  @override
  void update(void Function(RetryRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  RetryRequest build() => _build();

  _$RetryRequest _build() {
    final _$result = _$v ??
        _$RetryRequest._(
          jobId: BuiltValueNullFieldError.checkNotNull(
              jobId, r'RetryRequest', 'jobId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
