// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'resume_session_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ResumeSessionRequest extends ResumeSessionRequest {
  @override
  final String id;

  factory _$ResumeSessionRequest(
          [void Function(ResumeSessionRequestBuilder)? updates]) =>
      (ResumeSessionRequestBuilder()..update(updates))._build();

  _$ResumeSessionRequest._({required this.id}) : super._();
  @override
  ResumeSessionRequest rebuild(
          void Function(ResumeSessionRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ResumeSessionRequestBuilder toBuilder() =>
      ResumeSessionRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ResumeSessionRequest && id == other.id;
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
    return (newBuiltValueToStringHelper(r'ResumeSessionRequest')..add('id', id))
        .toString();
  }
}

class ResumeSessionRequestBuilder
    implements Builder<ResumeSessionRequest, ResumeSessionRequestBuilder> {
  _$ResumeSessionRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  ResumeSessionRequestBuilder() {
    ResumeSessionRequest._defaults(this);
  }

  ResumeSessionRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ResumeSessionRequest other) {
    _$v = other as _$ResumeSessionRequest;
  }

  @override
  void update(void Function(ResumeSessionRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ResumeSessionRequest build() => _build();

  _$ResumeSessionRequest _build() {
    final _$result = _$v ??
        _$ResumeSessionRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'ResumeSessionRequest', 'id'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
