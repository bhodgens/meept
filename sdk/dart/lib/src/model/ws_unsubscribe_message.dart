//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_collection/built_collection.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'ws_unsubscribe_message.g.dart';

/// WSUnsubscribeMessage
///
/// Properties:
/// * [type] 
/// * [channel] 
/// * [sessionId] 
@BuiltValue()
abstract class WSUnsubscribeMessage implements Built<WSUnsubscribeMessage, WSUnsubscribeMessageBuilder> {
  @BuiltValueField(wireName: r'type')
  WSUnsubscribeMessageTypeEnum? get type;
  // enum typeEnum {  unsubscribe,  };

  @BuiltValueField(wireName: r'channel')
  String? get channel;

  @BuiltValueField(wireName: r'session_id')
  String? get sessionId;

  WSUnsubscribeMessage._();

  factory WSUnsubscribeMessage([void updates(WSUnsubscribeMessageBuilder b)]) = _$WSUnsubscribeMessage;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(WSUnsubscribeMessageBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<WSUnsubscribeMessage> get serializer => _$WSUnsubscribeMessageSerializer();
}

class _$WSUnsubscribeMessageSerializer implements PrimitiveSerializer<WSUnsubscribeMessage> {
  @override
  final Iterable<Type> types = const [WSUnsubscribeMessage, _$WSUnsubscribeMessage];

  @override
  final String wireName = r'WSUnsubscribeMessage';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    WSUnsubscribeMessage object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.type != null) {
      yield r'type';
      yield serializers.serialize(
        object.type,
        specifiedType: const FullType(WSUnsubscribeMessageTypeEnum),
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
    WSUnsubscribeMessage object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required WSUnsubscribeMessageBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'type':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(WSUnsubscribeMessageTypeEnum),
          ) as WSUnsubscribeMessageTypeEnum;
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
  WSUnsubscribeMessage deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = WSUnsubscribeMessageBuilder();
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

class WSUnsubscribeMessageTypeEnum extends EnumClass {

  @BuiltValueEnumConst(wireName: r'unsubscribe')
  static const WSUnsubscribeMessageTypeEnum unsubscribe = _$wSUnsubscribeMessageTypeEnum_unsubscribe;

  static Serializer<WSUnsubscribeMessageTypeEnum> get serializer => _$wSUnsubscribeMessageTypeEnumSerializer;

  const WSUnsubscribeMessageTypeEnum._(String name): super(name);

  static BuiltSet<WSUnsubscribeMessageTypeEnum> get values => _$wSUnsubscribeMessageTypeEnumValues;
  static WSUnsubscribeMessageTypeEnum valueOf(String name) => _$wSUnsubscribeMessageTypeEnumValueOf(name);
}

