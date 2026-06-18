// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'invalidate_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$InvalidateRequest extends InvalidateRequest {
  @override
  final String? pathCommaOmitempty;

  factory _$InvalidateRequest(
          [void Function(InvalidateRequestBuilder)? updates]) =>
      (InvalidateRequestBuilder()..update(updates))._build();

  _$InvalidateRequest._({this.pathCommaOmitempty}) : super._();
  @override
  InvalidateRequest rebuild(void Function(InvalidateRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  InvalidateRequestBuilder toBuilder() =>
      InvalidateRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is InvalidateRequest &&
        pathCommaOmitempty == other.pathCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, pathCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'InvalidateRequest')
          ..add('pathCommaOmitempty', pathCommaOmitempty))
        .toString();
  }
}

class InvalidateRequestBuilder
    implements Builder<InvalidateRequest, InvalidateRequestBuilder> {
  _$InvalidateRequest? _$v;

  String? _pathCommaOmitempty;
  String? get pathCommaOmitempty => _$this._pathCommaOmitempty;
  set pathCommaOmitempty(String? pathCommaOmitempty) =>
      _$this._pathCommaOmitempty = pathCommaOmitempty;

  InvalidateRequestBuilder() {
    InvalidateRequest._defaults(this);
  }

  InvalidateRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _pathCommaOmitempty = $v.pathCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(InvalidateRequest other) {
    _$v = other as _$InvalidateRequest;
  }

  @override
  void update(void Function(InvalidateRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  InvalidateRequest build() => _build();

  _$InvalidateRequest _build() {
    final _$result = _$v ??
        _$InvalidateRequest._(
          pathCommaOmitempty: pathCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
