// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'compact_session_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CompactSessionRequest extends CompactSessionRequest {
  @override
  final String id;

  factory _$CompactSessionRequest(
          [void Function(CompactSessionRequestBuilder)? updates]) =>
      (CompactSessionRequestBuilder()..update(updates))._build();

  _$CompactSessionRequest._({required this.id}) : super._();
  @override
  CompactSessionRequest rebuild(
          void Function(CompactSessionRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CompactSessionRequestBuilder toBuilder() =>
      CompactSessionRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CompactSessionRequest && id == other.id;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CompactSessionRequest')
          ..add('id', id))
        .toString();
  }
}

class CompactSessionRequestBuilder
    implements Builder<CompactSessionRequest, CompactSessionRequestBuilder> {
  _$CompactSessionRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  CompactSessionRequestBuilder() {
    CompactSessionRequest._defaults(this);
  }

  CompactSessionRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CompactSessionRequest other) {
    _$v = other as _$CompactSessionRequest;
  }

  @override
  void update(void Function(CompactSessionRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CompactSessionRequest build() => _build();

  _$CompactSessionRequest _build() {
    final _$result = _$v ??
        _$CompactSessionRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'CompactSessionRequest', 'id'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
