// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'update_status_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$UpdateStatusRequest extends UpdateStatusRequest {
  @override
  final String pipelineId;
  @override
  final String status;

  factory _$UpdateStatusRequest(
          [void Function(UpdateStatusRequestBuilder)? updates]) =>
      (UpdateStatusRequestBuilder()..update(updates))._build();

  _$UpdateStatusRequest._({required this.pipelineId, required this.status})
      : super._();
  @override
  UpdateStatusRequest rebuild(
          void Function(UpdateStatusRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  UpdateStatusRequestBuilder toBuilder() =>
      UpdateStatusRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is UpdateStatusRequest &&
        pipelineId == other.pipelineId &&
        status == other.status;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, pipelineId.hashCode);
    _$hash = $jc(_$hash, status.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'UpdateStatusRequest')
          ..add('pipelineId', pipelineId)
          ..add('status', status))
        .toString();
  }
}

class UpdateStatusRequestBuilder
    implements Builder<UpdateStatusRequest, UpdateStatusRequestBuilder> {
  _$UpdateStatusRequest? _$v;

  String? _pipelineId;
  String? get pipelineId => _$this._pipelineId;
  set pipelineId(String? pipelineId) => _$this._pipelineId = pipelineId;

  String? _status;
  String? get status => _$this._status;
  set status(String? status) => _$this._status = status;

  UpdateStatusRequestBuilder() {
    UpdateStatusRequest._defaults(this);
  }

  UpdateStatusRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _pipelineId = $v.pipelineId;
      _status = $v.status;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(UpdateStatusRequest other) {
    _$v = other as _$UpdateStatusRequest;
  }

  @override
  void update(void Function(UpdateStatusRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  UpdateStatusRequest build() => _build();

  _$UpdateStatusRequest _build() {
    final _$result = _$v ??
        _$UpdateStatusRequest._(
          pipelineId: BuiltValueNullFieldError.checkNotNull(
              pipelineId, r'UpdateStatusRequest', 'pipelineId'),
          status: BuiltValueNullFieldError.checkNotNull(
              status, r'UpdateStatusRequest', 'status'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
