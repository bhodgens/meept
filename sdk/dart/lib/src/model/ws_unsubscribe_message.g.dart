// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'ws_unsubscribe_message.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

const WSUnsubscribeMessageTypeEnum _$wSUnsubscribeMessageTypeEnum_unsubscribe =
    const WSUnsubscribeMessageTypeEnum._('unsubscribe');

WSUnsubscribeMessageTypeEnum _$wSUnsubscribeMessageTypeEnumValueOf(
    String name) {
  switch (name) {
    case 'unsubscribe':
      return _$wSUnsubscribeMessageTypeEnum_unsubscribe;
    default:
      throw ArgumentError(name);
  }
}

final BuiltSet<WSUnsubscribeMessageTypeEnum>
    _$wSUnsubscribeMessageTypeEnumValues =
    BuiltSet<WSUnsubscribeMessageTypeEnum>(const <WSUnsubscribeMessageTypeEnum>[
  _$wSUnsubscribeMessageTypeEnum_unsubscribe,
]);

Serializer<WSUnsubscribeMessageTypeEnum>
    _$wSUnsubscribeMessageTypeEnumSerializer =
    _$WSUnsubscribeMessageTypeEnumSerializer();

class _$WSUnsubscribeMessageTypeEnumSerializer
    implements PrimitiveSerializer<WSUnsubscribeMessageTypeEnum> {
  static const Map<String, Object> _toWire = const <String, Object>{
    'unsubscribe': 'unsubscribe',
  };
  static const Map<Object, String> _fromWire = const <Object, String>{
    'unsubscribe': 'unsubscribe',
  };

  @override
  final Iterable<Type> types = const <Type>[WSUnsubscribeMessageTypeEnum];
  @override
  final String wireName = 'WSUnsubscribeMessageTypeEnum';

  @override
  Object serialize(Serializers serializers, WSUnsubscribeMessageTypeEnum object,
          {FullType specifiedType = FullType.unspecified}) =>
      _toWire[object.name] ?? object.name;

  @override
  WSUnsubscribeMessageTypeEnum deserialize(
          Serializers serializers, Object serialized,
          {FullType specifiedType = FullType.unspecified}) =>
      WSUnsubscribeMessageTypeEnum.valueOf(
          _fromWire[serialized] ?? (serialized is String ? serialized : ''));
}

class _$WSUnsubscribeMessage extends WSUnsubscribeMessage {
  @override
  final WSUnsubscribeMessageTypeEnum? type;
  @override
  final String? channel;
  @override
  final String? sessionId;

  factory _$WSUnsubscribeMessage(
          [void Function(WSUnsubscribeMessageBuilder)? updates]) =>
      (WSUnsubscribeMessageBuilder()..update(updates))._build();

  _$WSUnsubscribeMessage._({this.type, this.channel, this.sessionId})
      : super._();
  @override
  WSUnsubscribeMessage rebuild(
          void Function(WSUnsubscribeMessageBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  WSUnsubscribeMessageBuilder toBuilder() =>
      WSUnsubscribeMessageBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is WSUnsubscribeMessage &&
        type == other.type &&
        channel == other.channel &&
        sessionId == other.sessionId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, type.hashCode);
    _$hash = $jc(_$hash, channel.hashCode);
    _$hash = $jc(_$hash, sessionId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'WSUnsubscribeMessage')
          ..add('type', type)
          ..add('channel', channel)
          ..add('sessionId', sessionId))
        .toString();
  }
}

class WSUnsubscribeMessageBuilder
    implements Builder<WSUnsubscribeMessage, WSUnsubscribeMessageBuilder> {
  _$WSUnsubscribeMessage? _$v;

  WSUnsubscribeMessageTypeEnum? _type;
  WSUnsubscribeMessageTypeEnum? get type => _$this._type;
  set type(WSUnsubscribeMessageTypeEnum? type) => _$this._type = type;

  String? _channel;
  String? get channel => _$this._channel;
  set channel(String? channel) => _$this._channel = channel;

  String? _sessionId;
  String? get sessionId => _$this._sessionId;
  set sessionId(String? sessionId) => _$this._sessionId = sessionId;

  WSUnsubscribeMessageBuilder() {
    WSUnsubscribeMessage._defaults(this);
  }

  WSUnsubscribeMessageBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _type = $v.type;
      _channel = $v.channel;
      _sessionId = $v.sessionId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(WSUnsubscribeMessage other) {
    _$v = other as _$WSUnsubscribeMessage;
  }

  @override
  void update(void Function(WSUnsubscribeMessageBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  WSUnsubscribeMessage build() => _build();

  _$WSUnsubscribeMessage _build() {
    final _$result = _$v ??
        _$WSUnsubscribeMessage._(
          type: type,
          channel: channel,
          sessionId: sessionId,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
