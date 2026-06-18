// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'chat_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ChatService extends ChatService {
  @override
  final JsonObject? bus;
  @override
  final JsonObject? agentRegistry;
  @override
  final JsonObject? sessionStore;
  @override
  final JsonObject? logger;

  factory _$ChatService([void Function(ChatServiceBuilder)? updates]) =>
      (ChatServiceBuilder()..update(updates))._build();

  _$ChatService._(
      {this.bus, this.agentRegistry, this.sessionStore, this.logger})
      : super._();
  @override
  ChatService rebuild(void Function(ChatServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ChatServiceBuilder toBuilder() => ChatServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ChatService &&
        bus == other.bus &&
        agentRegistry == other.agentRegistry &&
        sessionStore == other.sessionStore &&
        logger == other.logger;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, bus.hashCode);
    _$hash = $jc(_$hash, agentRegistry.hashCode);
    _$hash = $jc(_$hash, sessionStore.hashCode);
    _$hash = $jc(_$hash, logger.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ChatService')
          ..add('bus', bus)
          ..add('agentRegistry', agentRegistry)
          ..add('sessionStore', sessionStore)
          ..add('logger', logger))
        .toString();
  }
}

class ChatServiceBuilder implements Builder<ChatService, ChatServiceBuilder> {
  _$ChatService? _$v;

  JsonObject? _bus;
  JsonObject? get bus => _$this._bus;
  set bus(JsonObject? bus) => _$this._bus = bus;

  JsonObject? _agentRegistry;
  JsonObject? get agentRegistry => _$this._agentRegistry;
  set agentRegistry(JsonObject? agentRegistry) =>
      _$this._agentRegistry = agentRegistry;

  JsonObject? _sessionStore;
  JsonObject? get sessionStore => _$this._sessionStore;
  set sessionStore(JsonObject? sessionStore) =>
      _$this._sessionStore = sessionStore;

  JsonObject? _logger;
  JsonObject? get logger => _$this._logger;
  set logger(JsonObject? logger) => _$this._logger = logger;

  ChatServiceBuilder() {
    ChatService._defaults(this);
  }

  ChatServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _bus = $v.bus;
      _agentRegistry = $v.agentRegistry;
      _sessionStore = $v.sessionStore;
      _logger = $v.logger;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ChatService other) {
    _$v = other as _$ChatService;
  }

  @override
  void update(void Function(ChatServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ChatService build() => _build();

  _$ChatService _build() {
    final _$result = _$v ??
        _$ChatService._(
          bus: bus,
          agentRegistry: agentRegistry,
          sessionStore: sessionStore,
          logger: logger,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
