// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'fork_session_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ForkSessionRequest extends ForkSessionRequest {
  @override
  final String sessionId;
  @override
  final int fromMessageId;
  @override
  final String? nameCommaOmitempty;

  factory _$ForkSessionRequest(
          [void Function(ForkSessionRequestBuilder)? updates]) =>
      (ForkSessionRequestBuilder()..update(updates))._build();

  _$ForkSessionRequest._(
      {required this.sessionId,
      required this.fromMessageId,
      this.nameCommaOmitempty})
      : super._();
  @override
  ForkSessionRequest rebuild(
          void Function(ForkSessionRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ForkSessionRequestBuilder toBuilder() =>
      ForkSessionRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ForkSessionRequest &&
        sessionId == other.sessionId &&
        fromMessageId == other.fromMessageId &&
        nameCommaOmitempty == other.nameCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, sessionId.hashCode);
    _$hash = $jc(_$hash, fromMessageId.hashCode);
    _$hash = $jc(_$hash, nameCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ForkSessionRequest')
          ..add('sessionId', sessionId)
          ..add('fromMessageId', fromMessageId)
          ..add('nameCommaOmitempty', nameCommaOmitempty))
        .toString();
  }
}

class ForkSessionRequestBuilder
    implements Builder<ForkSessionRequest, ForkSessionRequestBuilder> {
  _$ForkSessionRequest? _$v;

  String? _sessionId;
  String? get sessionId => _$this._sessionId;
  set sessionId(String? sessionId) => _$this._sessionId = sessionId;

  int? _fromMessageId;
  int? get fromMessageId => _$this._fromMessageId;
  set fromMessageId(int? fromMessageId) =>
      _$this._fromMessageId = fromMessageId;

  String? _nameCommaOmitempty;
  String? get nameCommaOmitempty => _$this._nameCommaOmitempty;
  set nameCommaOmitempty(String? nameCommaOmitempty) =>
      _$this._nameCommaOmitempty = nameCommaOmitempty;

  ForkSessionRequestBuilder() {
    ForkSessionRequest._defaults(this);
  }

  ForkSessionRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _sessionId = $v.sessionId;
      _fromMessageId = $v.fromMessageId;
      _nameCommaOmitempty = $v.nameCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ForkSessionRequest other) {
    _$v = other as _$ForkSessionRequest;
  }

  @override
  void update(void Function(ForkSessionRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ForkSessionRequest build() => _build();

  _$ForkSessionRequest _build() {
    final _$result = _$v ??
        _$ForkSessionRequest._(
          sessionId: BuiltValueNullFieldError.checkNotNull(
              sessionId, r'ForkSessionRequest', 'sessionId'),
          fromMessageId: BuiltValueNullFieldError.checkNotNull(
              fromMessageId, r'ForkSessionRequest', 'fromMessageId'),
          nameCommaOmitempty: nameCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
