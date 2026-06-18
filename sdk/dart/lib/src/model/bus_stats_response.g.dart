// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'bus_stats_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$BusStatsResponse extends BusStatsResponse {
  @override
  final int subscribers;
  @override
  final int messagesSent;
  @override
  final int queuedMessages;

  factory _$BusStatsResponse(
          [void Function(BusStatsResponseBuilder)? updates]) =>
      (BusStatsResponseBuilder()..update(updates))._build();

  _$BusStatsResponse._(
      {required this.subscribers,
      required this.messagesSent,
      required this.queuedMessages})
      : super._();
  @override
  BusStatsResponse rebuild(void Function(BusStatsResponseBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  BusStatsResponseBuilder toBuilder() =>
      BusStatsResponseBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is BusStatsResponse &&
        subscribers == other.subscribers &&
        messagesSent == other.messagesSent &&
        queuedMessages == other.queuedMessages;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, subscribers.hashCode);
    _$hash = $jc(_$hash, messagesSent.hashCode);
    _$hash = $jc(_$hash, queuedMessages.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'BusStatsResponse')
          ..add('subscribers', subscribers)
          ..add('messagesSent', messagesSent)
          ..add('queuedMessages', queuedMessages))
        .toString();
  }
}

class BusStatsResponseBuilder
    implements Builder<BusStatsResponse, BusStatsResponseBuilder> {
  _$BusStatsResponse? _$v;

  int? _subscribers;
  int? get subscribers => _$this._subscribers;
  set subscribers(int? subscribers) => _$this._subscribers = subscribers;

  int? _messagesSent;
  int? get messagesSent => _$this._messagesSent;
  set messagesSent(int? messagesSent) => _$this._messagesSent = messagesSent;

  int? _queuedMessages;
  int? get queuedMessages => _$this._queuedMessages;
  set queuedMessages(int? queuedMessages) =>
      _$this._queuedMessages = queuedMessages;

  BusStatsResponseBuilder() {
    BusStatsResponse._defaults(this);
  }

  BusStatsResponseBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _subscribers = $v.subscribers;
      _messagesSent = $v.messagesSent;
      _queuedMessages = $v.queuedMessages;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(BusStatsResponse other) {
    _$v = other as _$BusStatsResponse;
  }

  @override
  void update(void Function(BusStatsResponseBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  BusStatsResponse build() => _build();

  _$BusStatsResponse _build() {
    final _$result = _$v ??
        _$BusStatsResponse._(
          subscribers: BuiltValueNullFieldError.checkNotNull(
              subscribers, r'BusStatsResponse', 'subscribers'),
          messagesSent: BuiltValueNullFieldError.checkNotNull(
              messagesSent, r'BusStatsResponse', 'messagesSent'),
          queuedMessages: BuiltValueNullFieldError.checkNotNull(
              queuedMessages, r'BusStatsResponse', 'queuedMessages'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
