// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'get_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$GetRequest extends GetRequest {
  @override
  final String jobId;

  factory _$GetRequest([void Function(GetRequestBuilder)? updates]) =>
      (GetRequestBuilder()..update(updates))._build();

  _$GetRequest._({required this.jobId}) : super._();
  @override
  GetRequest rebuild(void Function(GetRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  GetRequestBuilder toBuilder() => GetRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is GetRequest && jobId == other.jobId;
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
    return (newBuiltValueToStringHelper(r'GetRequest')..add('jobId', jobId))
        .toString();
  }
}

class GetRequestBuilder implements Builder<GetRequest, GetRequestBuilder> {
  _$GetRequest? _$v;

  String? _jobId;
  String? get jobId => _$this._jobId;
  set jobId(String? jobId) => _$this._jobId = jobId;

  GetRequestBuilder() {
    GetRequest._defaults(this);
  }

  GetRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _jobId = $v.jobId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(GetRequest other) {
    _$v = other as _$GetRequest;
  }

  @override
  void update(void Function(GetRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  GetRequest build() => _build();

  _$GetRequest _build() {
    final _$result = _$v ??
        _$GetRequest._(
          jobId: BuiltValueNullFieldError.checkNotNull(
              jobId, r'GetRequest', 'jobId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
