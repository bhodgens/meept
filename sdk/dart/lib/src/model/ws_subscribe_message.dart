//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_collection/built_collection.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'ws_subscribe_message.g.dart';

/// WSSubscribeMessage
///
/// Properties:
/// * [type] 
/// * [channel] 
/// * [sessionId] 
@BuiltValue()
abstract class WSSubscribeMessage implements Built<WSSubscribeMessage, WSSubscribeMessageBuilder> {
  @BuiltValueField(wireName: r'type')
  WSSubscribeMessageTypeEnum? get type;
  // enum typeEnum {  subscribe,  };

  @BuiltValueField(wireName: r'channel')
  String? get channel;

  @BuiltValueField(wireName: r'session_id')
  String? get sessionId;

  WSSubscribeMessage._();

  factory WSSubscribeMessage([void updates(WSSubscribeMessageBuilder b)]) = _$WSSubscribeMessage;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(WSSubscribeMessageBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<WSSubscribeMessage> get serializer => _$WSSubscribeMessageSerializer();
}

class _$WSSubscribeMessageSerializer implements PrimitiveSerializer<WSSubscribeMessage> {
  @override
  final Iterable<Type> types = const [WSSubscribeMessage, _$WSSubscribeMessage];

  @override
  final String wireName = r'WSSubscribeMessage';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    WSSubscribeMessage object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.type != null) {
      yield r'type';
      yield serializers.serialize(
        object.type,
        specifiedType: const FullType(WSSubscribeMessageTypeEnum),
      );
    }
    if (object.channel != null) {
      yield r'channel';
      yield serializers.serialize(
        object.channel,
        specifiedType: const FullType(String),
      );
    }
    if (object.sessionId != null) {
      yield r'session_id';
      yield serializers.serialize(
        object.sessionId,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    WSSubscribeMessage object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required WSSubscribeMessageBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'type':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(WSSubscribeMessageTypeEnum),
          ) as WSSubscribeMessageTypeEnum;
          result.type = valueDes;
          break;
        case r'channel':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.channel = valueDes;
          break;
        case r'session_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.sessionId = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  WSSubscribeMessage deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = WSSubscribeMessageBuilder();
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

class WSSubscribeMessageTypeEnum extends EnumClass {

  @BuiltValueEnumConst(wireName: r'subscribe')
  static const WSSubscribeMessageTypeEnum subscribe = _$wSSubscribeMessageTypeEnum_subscribe;

  static Serializer<WSSubscribeMessageTypeEnum> get serializer => _$wSSubscribeMessageTypeEnumSerializer;

  const WSSubscribeMessageTypeEnum._(String name): super(name);

  static BuiltSet<WSSubscribeMessageTypeEnum> get values => _$wSSubscribeMessageTypeEnumValues;
  static WSSubscribeMessageTypeEnum valueOf(String name) => _$wSSubscribeMessageTypeEnumValueOf(name);
}

