// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'steer_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SteerRequest extends SteerRequest {
  @override
  final String message;
  @override
  final String conversationId;
  @override
  final String? sourceCommaOmitempty;

  factory _$SteerRequest([void Function(SteerRequestBuilder)? updates]) =>
      (SteerRequestBuilder()..update(updates))._build();

  _$SteerRequest._(
      {required this.message,
      required this.conversationId,
      this.sourceCommaOmitempty})
      : super._();
  @override
  SteerRequest rebuild(void Function(SteerRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SteerRequestBuilder toBuilder() => SteerRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SteerRequest &&
        message == other.message &&
        conversationId == other.conversationId &&
        sourceCommaOmitempty == other.sourceCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, message.hashCode);
    _$hash = $jc(_$hash, conversationId.hashCode);
    _$hash = $jc(_$hash, sourceCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SteerRequest')
          ..add('message', message)
          ..add('conversationId', conversationId)
          ..add('sourceCommaOmitempty', sourceCommaOmitempty))
        .toString();
  }
}

class SteerRequestBuilder
    implements Builder<SteerRequest, SteerRequestBuilder> {
  _$SteerRequest? _$v;

  String? _message;
  String? get message => _$this._message;
  set message(String? message) => _$this._message = message;

  String? _conversationId;
  String? get conversationId => _$this._conversationId;
  set conversationId(String? conversationId) =>
      _$this._conversationId = conversationId;

  String? _sourceCommaOmitempty;
  String? get sourceCommaOmitempty => _$this._sourceCommaOmitempty;
  set sourceCommaOmitempty(String? sourceCommaOmitempty) =>
      _$this._sourceCommaOmitempty = sourceCommaOmitempty;

  SteerRequestBuilder() {
    SteerRequest._defaults(this);
  }

  SteerRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _message = $v.message;
      _conversationId = $v.conversationId;
      _sourceCommaOmitempty = $v.sourceCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SteerRequest other) {
    _$v = other as _$SteerRequest;
  }

  @override
  void update(void Function(SteerRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SteerRequest build() => _build();

  _$SteerRequest _build() {
    final _$result = _$v ??
        _$SteerRequest._(
          message: BuiltValueNullFieldError.checkNotNull(
              message, r'SteerRequest', 'message'),
          conversationId: BuiltValueNullFieldError.checkNotNull(
              conversationId, r'SteerRequest', 'conversationId'),
          sourceCommaOmitempty: sourceCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
