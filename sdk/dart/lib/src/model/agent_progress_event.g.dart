// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'agent_progress_event.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

const AgentProgressEventTypeEnum _$agentProgressEventTypeEnum_agentProgress =
    const AgentProgressEventTypeEnum._('agentProgress');

AgentProgressEventTypeEnum _$agentProgressEventTypeEnumValueOf(String name) {
  switch (name) {
    case 'agentProgress':
      return _$agentProgressEventTypeEnum_agentProgress;
    default:
      throw ArgumentError(name);
  }
}

final BuiltSet<AgentProgressEventTypeEnum> _$agentProgressEventTypeEnumValues =
    BuiltSet<AgentProgressEventTypeEnum>(const <AgentProgressEventTypeEnum>[
  _$agentProgressEventTypeEnum_agentProgress,
]);

Serializer<AgentProgressEventTypeEnum> _$agentProgressEventTypeEnumSerializer =
    _$AgentProgressEventTypeEnumSerializer();

class _$AgentProgressEventTypeEnumSerializer
    implements PrimitiveSerializer<AgentProgressEventTypeEnum> {
  static const Map<String, Object> _toWire = const <String, Object>{
    'agentProgress': 'agent_progress',
  };
  static const Map<Object, String> _fromWire = const <Object, String>{
    'agent_progress': 'agentProgress',
  };

  @override
  final Iterable<Type> types = const <Type>[AgentProgressEventTypeEnum];
  @override
  final String wireName = 'AgentProgressEventTypeEnum';

  @override
  Object serialize(Serializers serializers, AgentProgressEventTypeEnum object,
          {FullType specifiedType = FullType.unspecified}) =>
      _toWire[object.name] ?? object.name;

  @override
  AgentProgressEventTypeEnum deserialize(
          Serializers serializers, Object serialized,
          {FullType specifiedType = FullType.unspecified}) =>
      AgentProgressEventTypeEnum.valueOf(
          _fromWire[serialized] ?? (serialized is String ? serialized : ''));
}

class _$AgentProgressEvent extends AgentProgressEvent {
  @override
  final AgentProgressEventTypeEnum? type;
  @override
  final String? sessionId;
  @override
  final String? agentId;
  @override
  final String? message;
  @override
  final int? tier;
  @override
  final String? sourceEvent;
  @override
  final DateTime? timestamp;

  factory _$AgentProgressEvent(
          [void Function(AgentProgressEventBuilder)? updates]) =>
      (AgentProgressEventBuilder()..update(updates))._build();

  _$AgentProgressEvent._(
      {this.type,
      this.sessionId,
      this.agentId,
      this.message,
      this.tier,
      this.sourceEvent,
      this.timestamp})
      : super._();
  @override
  AgentProgressEvent rebuild(
          void Function(AgentProgressEventBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  AgentProgressEventBuilder toBuilder() =>
      AgentProgressEventBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is AgentProgressEvent &&
        type == other.type &&
        sessionId == other.sessionId &&
        agentId == other.agentId &&
        message == other.message &&
        tier == other.tier &&
        sourceEvent == other.sourceEvent &&
        timestamp == other.timestamp;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, type.hashCode);
    _$hash = $jc(_$hash, sessionId.hashCode);
    _$hash = $jc(_$hash, agentId.hashCode);
    _$hash = $jc(_$hash, message.hashCode);
    _$hash = $jc(_$hash, tier.hashCode);
    _$hash = $jc(_$hash, sourceEvent.hashCode);
    _$hash = $jc(_$hash, timestamp.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'AgentProgressEvent')
          ..add('type', type)
          ..add('sessionId', sessionId)
          ..add('agentId', agentId)
          ..add('message', message)
          ..add('tier', tier)
          ..add('sourceEvent', sourceEvent)
          ..add('timestamp', timestamp))
        .toString();
  }
}

class AgentProgressEventBuilder
    implements Builder<AgentProgressEvent, AgentProgressEventBuilder> {
  _$AgentProgressEvent? _$v;

  AgentProgressEventTypeEnum? _type;
  AgentProgressEventTypeEnum? get type => _$this._type;
  set type(AgentProgressEventTypeEnum? type) => _$this._type = type;

  String? _sessionId;
  String? get sessionId => _$this._sessionId;
  set sessionId(String? sessionId) => _$this._sessionId = sessionId;

  String? _agentId;
  String? get agentId => _$this._agentId;
  set agentId(String? agentId) => _$this._agentId = agentId;

  String? _message;
  String? get message => _$this._message;
  set message(String? message) => _$this._message = message;

  int? _tier;
  int? get tier => _$this._tier;
  set tier(int? tier) => _$this._tier = tier;

  String? _sourceEvent;
  String? get sourceEvent => _$this._sourceEvent;
  set sourceEvent(String? sourceEvent) => _$this._sourceEvent = sourceEvent;

  DateTime? _timestamp;
  DateTime? get timestamp => _$this._timestamp;
  set timestamp(DateTime? timestamp) => _$this._timestamp = timestamp;

  AgentProgressEventBuilder() {
    AgentProgressEvent._defaults(this);
  }

  AgentProgressEventBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _type = $v.type;
      _sessionId = $v.sessionId;
      _agentId = $v.agentId;
      _message = $v.message;
      _tier = $v.tier;
      _sourceEvent = $v.sourceEvent;
      _timestamp = $v.timestamp;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(AgentProgressEvent other) {
    _$v = other as _$AgentProgressEvent;
  }

  @override
  void update(void Function(AgentProgressEventBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  AgentProgressEvent build() => _build();

  _$AgentProgressEvent _build() {
    final _$result = _$v ??
        _$AgentProgressEvent._(
          type: type,
          sessionId: sessionId,
          agentId: agentId,
          message: message,
          tier: tier,
          sourceEvent: sourceEvent,
          timestamp: timestamp,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
