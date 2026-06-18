// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'delete_pipeline_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$DeletePipelineRequest extends DeletePipelineRequest {
  @override
  final String pipelineId;

  factory _$DeletePipelineRequest(
          [void Function(DeletePipelineRequestBuilder)? updates]) =>
      (DeletePipelineRequestBuilder()..update(updates))._build();

  _$DeletePipelineRequest._({required this.pipelineId}) : super._();
  @override
  DeletePipelineRequest rebuild(
          void Function(DeletePipelineRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  DeletePipelineRequestBuilder toBuilder() =>
      DeletePipelineRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is DeletePipelineRequest && pipelineId == other.pipelineId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, pipelineId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'DeletePipelineRequest')
          ..add('pipelineId', pipelineId))
        .toString();
  }
}

class DeletePipelineRequestBuilder
    implements Builder<DeletePipelineRequest, DeletePipelineRequestBuilder> {
  _$DeletePipelineRequest? _$v;

  String? _pipelineId;
  String? get pipelineId => _$this._pipelineId;
  set pipelineId(String? pipelineId) => _$this._pipelineId = pipelineId;

  DeletePipelineRequestBuilder() {
    DeletePipelineRequest._defaults(this);
  }

  DeletePipelineRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _pipelineId = $v.pipelineId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(DeletePipelineRequest other) {
    _$v = other as _$DeletePipelineRequest;
  }

  @override
  void update(void Function(DeletePipelineRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  DeletePipelineRequest build() => _build();

  _$DeletePipelineRequest _build() {
    final _$result = _$v ??
        _$DeletePipelineRequest._(
          pipelineId: BuiltValueNullFieldError.checkNotNull(
              pipelineId, r'DeletePipelineRequest', 'pipelineId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
