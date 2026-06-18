// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'fail_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$FailRequest extends FailRequest {
  @override
  final String jobId;
  @override
  final String error;

  factory _$FailRequest([void Function(FailRequestBuilder)? updates]) =>
      (FailRequestBuilder()..update(updates))._build();

  _$FailRequest._({required this.jobId, required this.error}) : super._();
  @override
  FailRequest rebuild(void Function(FailRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  FailRequestBuilder toBuilder() => FailRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is FailRequest && jobId == other.jobId && error == other.error;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, jobId.hashCode);
    _$hash = $jc(_$hash, error.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'FailRequest')
          ..add('jobId', jobId)
          ..add('error', error))
        .toString();
  }
}

class FailRequestBuilder implements Builder<FailRequest, FailRequestBuilder> {
  _$FailRequest? _$v;

  String? _jobId;
  String? get jobId => _$this._jobId;
  set jobId(String? jobId) => _$this._jobId = jobId;

  String? _error;
  String? get error => _$this._error;
  set error(String? error) => _$this._error = error;

  FailRequestBuilder() {
    FailRequest._defaults(this);
  }

  FailRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _jobId = $v.jobId;
      _error = $v.error;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(FailRequest other) {
    _$v = other as _$FailRequest;
  }

  @override
  void update(void Function(FailRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  FailRequest build() => _build();

  _$FailRequest _build() {
    final _$result = _$v ??
        _$FailRequest._(
          jobId: BuiltValueNullFieldError.checkNotNull(
              jobId, r'FailRequest', 'jobId'),
          error: BuiltValueNullFieldError.checkNotNull(
              error, r'FailRequest', 'error'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
