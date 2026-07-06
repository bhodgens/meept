// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'follow_up_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$FollowUpRequest extends FollowUpRequest {
  @override
  final String message;
  @override
  final String conversationId;
  @override
  final String? source;

  factory _$FollowUpRequest([void Function(FollowUpRequestBuilder)? updates]) =>
      (FollowUpRequestBuilder()..update(updates))._build();

  _$FollowUpRequest._(
      {required this.message,
      required this.conversationId,
      this.source})
      : super._();
  @override
  FollowUpRequest rebuild(void Function(FollowUpRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  FollowUpRequestBuilder toBuilder() => FollowUpRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is FollowUpRequest &&
        message == other.message &&
        conversationId == other.conversationId &&
        source == other.source;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, message.hashCode);
    _$hash = $jc(_$hash, conversationId.hashCode);
    _$hash = $jc(_$hash, source.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'FollowUpRequest')
          ..add('message', message)
          ..add('conversationId', conversationId)
          ..add('source', source))
        .toString();
  }
}

class FollowUpRequestBuilder
    implements Builder<FollowUpRequest, FollowUpRequestBuilder> {
  _$FollowUpRequest? _$v;

  String? _message;
  String? get message => _$this._message;
  set message(String? message) => _$this._message = message;

  String? _conversationId;
  String? get conversationId => _$this._conversationId;
  set conversationId(String? conversationId) =>
      _$this._conversationId = conversationId;

  String? _source;
  String? get source => _$this._source;
  set source(String? source) =>
      _$this._source = source;

  FollowUpRequestBuilder() {
    FollowUpRequest._defaults(this);
  }

  FollowUpRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _message = $v.message;
      _conversationId = $v.conversationId;
      _source = $v.source;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(FollowUpRequest other) {
    _$v = other as _$FollowUpRequest;
  }

  @override
  void update(void Function(FollowUpRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  FollowUpRequest build() => _build();

  _$FollowUpRequest _build() {
    final _$result = _$v ??
        _$FollowUpRequest._(
          message: BuiltValueNullFieldError.checkNotNull(
              message, r'FollowUpRequest', 'message'),
          conversationId: BuiltValueNullFieldError.checkNotNull(
              conversationId, r'FollowUpRequest', 'conversationId'),
          source: source,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
