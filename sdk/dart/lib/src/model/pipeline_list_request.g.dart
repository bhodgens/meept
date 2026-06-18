// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'pipeline_list_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$PipelineListRequest extends PipelineListRequest {
  @override
  final int? limitCommaOmitempty;

  factory _$PipelineListRequest(
          [void Function(PipelineListRequestBuilder)? updates]) =>
      (PipelineListRequestBuilder()..update(updates))._build();

  _$PipelineListRequest._({this.limitCommaOmitempty}) : super._();
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
        limitCommaOmitempty == other.limitCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, limitCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'PipelineListRequest')
          ..add('limitCommaOmitempty', limitCommaOmitempty))
        .toString();
  }
}

class PipelineListRequestBuilder
    implements Builder<PipelineListRequest, PipelineListRequestBuilder> {
  _$PipelineListRequest? _$v;

  int? _limitCommaOmitempty;
  int? get limitCommaOmitempty => _$this._limitCommaOmitempty;
  set limitCommaOmitempty(int? limitCommaOmitempty) =>
      _$this._limitCommaOmitempty = limitCommaOmitempty;

  PipelineListRequestBuilder() {
    PipelineListRequest._defaults(this);
  }

  PipelineListRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _limitCommaOmitempty = $v.limitCommaOmitempty;
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
          limitCommaOmitempty: limitCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
