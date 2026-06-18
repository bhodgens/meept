// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'templates_list_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TemplatesListRequest extends TemplatesListRequest {
  @override
  final int? limitCommaOmitempty;

  factory _$TemplatesListRequest(
          [void Function(TemplatesListRequestBuilder)? updates]) =>
      (TemplatesListRequestBuilder()..update(updates))._build();

  _$TemplatesListRequest._({this.limitCommaOmitempty}) : super._();
  @override
  TemplatesListRequest rebuild(
          void Function(TemplatesListRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TemplatesListRequestBuilder toBuilder() =>
      TemplatesListRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TemplatesListRequest &&
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
    return (newBuiltValueToStringHelper(r'TemplatesListRequest')
          ..add('limitCommaOmitempty', limitCommaOmitempty))
        .toString();
  }
}

class TemplatesListRequestBuilder
    implements Builder<TemplatesListRequest, TemplatesListRequestBuilder> {
  _$TemplatesListRequest? _$v;

  int? _limitCommaOmitempty;
  int? get limitCommaOmitempty => _$this._limitCommaOmitempty;
  set limitCommaOmitempty(int? limitCommaOmitempty) =>
      _$this._limitCommaOmitempty = limitCommaOmitempty;

  TemplatesListRequestBuilder() {
    TemplatesListRequest._defaults(this);
  }

  TemplatesListRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _limitCommaOmitempty = $v.limitCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TemplatesListRequest other) {
    _$v = other as _$TemplatesListRequest;
  }

  @override
  void update(void Function(TemplatesListRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TemplatesListRequest build() => _build();

  _$TemplatesListRequest _build() {
    final _$result = _$v ??
        _$TemplatesListRequest._(
          limitCommaOmitempty: limitCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
