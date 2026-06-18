//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'queue_service.g.dart';

/// QueueService
///
/// Properties:
/// * [q] 
@BuiltValue()
abstract class QueueService implements Built<QueueService, QueueServiceBuilder> {
  @BuiltValueField(wireName: r'q')
  JsonObject? get q;

  QueueService._();

  factory QueueService([void updates(QueueServiceBuilder b)]) = _$QueueService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(QueueServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<QueueService> get serializer => _$QueueServiceSerializer();
}

class _$QueueServiceSerializer implements PrimitiveSerializer<QueueService> {
  @override
  final Iterable<Type> types = const [QueueService, _$QueueService];

  @override
  final String wireName = r'QueueService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    QueueService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.q != null) {
      yield r'q';
      yield serializers.serialize(
        object.q,
        specifiedType: const FullType(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    QueueService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required QueueServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'q':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.q = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  QueueService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = QueueServiceBuilder();
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

