// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'status_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$StatusRequest extends StatusRequest {
  @override
  final String pipelineId;

  factory _$StatusRequest([void Function(StatusRequestBuilder)? updates]) =>
      (StatusRequestBuilder()..update(updates))._build();

  _$StatusRequest._({required this.pipelineId}) : super._();
  @override
  StatusRequest rebuild(void Function(StatusRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  StatusRequestBuilder toBuilder() => StatusRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is StatusRequest && pipelineId == other.pipelineId;
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
    return (newBuiltValueToStringHelper(r'StatusRequest')
          ..add('pipelineId', pipelineId))
        .toString();
  }
}

class StatusRequestBuilder
    implements Builder<StatusRequest, StatusRequestBuilder> {
  _$StatusRequest? _$v;

  String? _pipelineId;
  String? get pipelineId => _$this._pipelineId;
  set pipelineId(String? pipelineId) => _$this._pipelineId = pipelineId;

  StatusRequestBuilder() {
    StatusRequest._defaults(this);
  }

  StatusRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _pipelineId = $v.pipelineId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(StatusRequest other) {
    _$v = other as _$StatusRequest;
  }

  @override
  void update(void Function(StatusRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  StatusRequest build() => _build();

  _$StatusRequest _build() {
    final _$result = _$v ??
        _$StatusRequest._(
          pipelineId: BuiltValueNullFieldError.checkNotNull(
              pipelineId, r'StatusRequest', 'pipelineId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
