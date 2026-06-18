//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'chat_service.g.dart';

/// ChatService
///
/// Properties:
/// * [bus] 
/// * [agentRegistry] 
/// * [sessionStore] 
/// * [logger] 
@BuiltValue()
abstract class ChatService implements Built<ChatService, ChatServiceBuilder> {
  @BuiltValueField(wireName: r'bus')
  JsonObject? get bus;

  @BuiltValueField(wireName: r'agentRegistry')
  JsonObject? get agentRegistry;

  @BuiltValueField(wireName: r'sessionStore')
  JsonObject? get sessionStore;

  @BuiltValueField(wireName: r'logger')
  JsonObject? get logger;

  ChatService._();

  factory ChatService([void updates(ChatServiceBuilder b)]) = _$ChatService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ChatServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ChatService> get serializer => _$ChatServiceSerializer();
}

class _$ChatServiceSerializer implements PrimitiveSerializer<ChatService> {
  @override
  final Iterable<Type> types = const [ChatService, _$ChatService];

  @override
  final String wireName = r'ChatService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ChatService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.bus != null) {
      yield r'bus';
      yield serializers.serialize(
        object.bus,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.agentRegistry != null) {
      yield r'agentRegistry';
      yield serializers.serialize(
        object.agentRegistry,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.sessionStore != null) {
      yield r'sessionStore';
      yield serializers.serialize(
        object.sessionStore,
        specifiedType: const FullType(JsonObject),
      );
    }
    if (object.logger != null) {
      yield r'logger';
      yield serializers.serialize(
        object.logger,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    ChatService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ChatServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'bus':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.bus = valueDes;
          break;
        case r'agentRegistry':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.agentRegistry = valueDes;
          break;
        case r'sessionStore':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.sessionStore = valueDes;
          break;
        case r'logger':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.logger = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ChatService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ChatServiceBuilder();
    final serializedList = (serialized as Iterable<Object?>).toList();
    final unhandled = <Object?>[];
    _deserializeProperties(
      serializers,
      serialized,
      specifiedType: specifiedType,
      serializedList: serializedList,
      unhandled: unhandled,
      result: result,
    );
    return result.build();
  }
}

