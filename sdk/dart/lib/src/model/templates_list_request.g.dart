// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'templates_list_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TemplatesListRequest extends TemplatesListRequest {
  @override
  final int? limit;

  factory _$TemplatesListRequest(
          [void Function(TemplatesListRequestBuilder)? updates]) =>
      (TemplatesListRequestBuilder()..update(updates))._build();

  _$TemplatesListRequest._({this.limit}) : super._();
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
    return (newBuiltValueToStringHelper(r'TemplatesListRequest')
          ..add('limit', limit))
        .toString();
  }
}

class TemplatesListRequestBuilder
    implements Builder<TemplatesListRequest, TemplatesListRequestBuilder> {
  _$TemplatesListRequest? _$v;

  int? _limit;
  int? get limit => _$this._limit;
  set limit(int? limit) =>
      _$this._limit = limit;

  TemplatesListRequestBuilder() {
    TemplatesListRequest._defaults(this);
  }

  TemplatesListRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _limit = $v.limit;
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
          limit: limit,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
