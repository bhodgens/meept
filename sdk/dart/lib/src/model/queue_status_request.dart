//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'queue_status_request.g.dart';

/// QueueStatusRequest
///
/// Properties:
/// * [conversationId] 
@BuiltValue()
abstract class QueueStatusRequest implements Built<QueueStatusRequest, QueueStatusRequestBuilder> {
  @BuiltValueField(wireName: r'conversation_id')
  String get conversationId;

  QueueStatusRequest._();

  factory QueueStatusRequest([void updates(QueueStatusRequestBuilder b)]) = _$QueueStatusRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(QueueStatusRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<QueueStatusRequest> get serializer => _$QueueStatusRequestSerializer();
}

class _$QueueStatusRequestSerializer implements PrimitiveSerializer<QueueStatusRequest> {
  @override
  final Iterable<Type> types = const [QueueStatusRequest, _$QueueStatusRequest];

  @override
  final String wireName = r'QueueStatusRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    QueueStatusRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'conversation_id';
    yield serializers.serialize(
      object.conversationId,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    QueueStatusRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required QueueStatusRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'conversation_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.conversationId = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  QueueStatusRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = QueueStatusRequestBuilder();
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

