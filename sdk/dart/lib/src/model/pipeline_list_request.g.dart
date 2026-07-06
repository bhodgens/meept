// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'pipeline_list_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$PipelineListRequest extends PipelineListRequest {
  @override
  final int? limit;

  factory _$PipelineListRequest(
          [void Function(PipelineListRequestBuilder)? updates]) =>
      (PipelineListRequestBuilder()..update(updates))._build();

  _$PipelineListRequest._({this.limit}) : super._();
  @override
  PipelineListRequest rebuild(
          void Function(PipelineListRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  PipelineListRequestBuilder toBuilder() =>
      PipelineListRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is PipelineListRequest &&
        limit == other.limit;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, limit.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'PipelineListRequest')
          ..add('limit', limit))
        .toString();
  }
}

class PipelineListRequestBuilder
    implements Builder<PipelineListRequest, PipelineListRequestBuilder> {
  _$PipelineListRequest? _$v;

  int? _limit;
  int? get limit => _$this._limit;
  set limit(int? limit) =>
      _$this._limit = limit;

  PipelineListRequestBuilder() {
    PipelineListRequest._defaults(this);
  }

  PipelineListRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _limit = $v.limit;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(PipelineListRequest other) {
    _$v = other as _$PipelineListRequest;
  }

  @override
  void update(void Function(PipelineListRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  PipelineListRequest build() => _build();

  _$PipelineListRequest _build() {
    final _$result = _$v ??
        _$PipelineListRequest._(
          limit: limit,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
