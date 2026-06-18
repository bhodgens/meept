// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'ws_subscribe_message.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

const WSSubscribeMessageTypeEnum _$wSSubscribeMessageTypeEnum_subscribe =
    const WSSubscribeMessageTypeEnum._('subscribe');

WSSubscribeMessageTypeEnum _$wSSubscribeMessageTypeEnumValueOf(String name) {
  switch (name) {
    case 'subscribe':
      return _$wSSubscribeMessageTypeEnum_subscribe;
    default:
      throw ArgumentError(name);
  }
}

final BuiltSet<WSSubscribeMessageTypeEnum> _$wSSubscribeMessageTypeEnumValues =
    BuiltSet<WSSubscribeMessageTypeEnum>(const <WSSubscribeMessageTypeEnum>[
  _$wSSubscribeMessageTypeEnum_subscribe,
]);

Serializer<WSSubscribeMessageTypeEnum> _$wSSubscribeMessageTypeEnumSerializer =
    _$WSSubscribeMessageTypeEnumSerializer();

class _$WSSubscribeMessageTypeEnumSerializer
    implements PrimitiveSerializer<WSSubscribeMessageTypeEnum> {
  static const Map<String, Object> _toWire = const <String, Object>{
    'subscribe': 'subscribe',
  };
  static const Map<Object, String> _fromWire = const <Object, String>{
    'subscribe': 'subscribe',
  };

  @override
  final Iterable<Type> types = const <Type>[WSSubscribeMessageTypeEnum];
  @override
  final String wireName = 'WSSubscribeMessageTypeEnum';

  @override
  Object serialize(Serializers serializers, WSSubscribeMessageTypeEnum object,
          {FullType specifiedType = FullType.unspecified}) =>
      _toWire[object.name] ?? object.name;

  @override
  WSSubscribeMessageTypeEnum deserialize(
          Serializers serializers, Object serialized,
          {FullType specifiedType = FullType.unspecified}) =>
      WSSubscribeMessageTypeEnum.valueOf(
          _fromWire[serialized] ?? (serialized is String ? serialized : ''));
}

class _$WSSubscribeMessage extends WSSubscribeMessage {
  @override
  final WSSubscribeMessageTypeEnum? type;
  @override
  final String? channel;
  @override
  final String? sessionId;

  factory _$WSSubscribeMessage(
          [void Function(WSSubscribeMessageBuilder)? updates]) =>
      (WSSubscribeMessageBuilder()..update(updates))._build();

  _$WSSubscribeMessage._({this.type, this.channel, this.sessionId}) : super._();
  @override
  WSSubscribeMessage rebuild(
          void Function(WSSubscribeMessageBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  WSSubscribeMessageBuilder toBuilder() =>
      WSSubscribeMessageBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is WSSubscribeMessage &&
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
    return (newBuiltValueToStringHelper(r'WSSubscribeMessage')
          ..add('type', type)
          ..add('channel', channel)
          ..add('sessionId', sessionId))
        .toString();
  }
}

class WSSubscribeMessageBuilder
    implements Builder<WSSubscribeMessage, WSSubscribeMessageBuilder> {
  _$WSSubscribeMessage? _$v;

  WSSubscribeMessageTypeEnum? _type;
  WSSubscribeMessageTypeEnum? get type => _$this._type;
  set type(WSSubscribeMessageTypeEnum? type) => _$this._type = type;

  String? _channel;
  String? get channel => _$this._channel;
  set channel(String? channel) => _$this._channel = channel;

  String? _sessionId;
  String? get sessionId => _$this._sessionId;
  set sessionId(String? sessionId) => _$this._sessionId = sessionId;

  WSSubscribeMessageBuilder() {
    WSSubscribeMessage._defaults(this);
  }

  WSSubscribeMessageBuilder get _$this {
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
  void replace(WSSubscribeMessage other) {
    _$v = other as _$WSSubscribeMessage;
  }

  @override
  void update(void Function(WSSubscribeMessageBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  WSSubscribeMessage build() => _build();

  _$WSSubscribeMessage _build() {
    final _$result = _$v ??
        _$WSSubscribeMessage._(
          type: type,
          channel: channel,
          sessionId: sessionId,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
