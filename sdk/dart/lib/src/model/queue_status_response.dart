//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'queue_status_response.g.dart';

/// QueueStatusResponse
///
/// Properties:
/// * [steeringDepth] 
/// * [followupDepth] 
/// * [isActive] 
/// * [generation] 
@BuiltValue()
abstract class QueueStatusResponse implements Built<QueueStatusResponse, QueueStatusResponseBuilder> {
  @BuiltValueField(wireName: r'steering_depth')
  int get steeringDepth;

  @BuiltValueField(wireName: r'followup_depth')
  int get followupDepth;

  @BuiltValueField(wireName: r'is_active')
  bool get isActive;

  @BuiltValueField(wireName: r'generation')
  int get generation;

  QueueStatusResponse._();

  factory QueueStatusResponse([void updates(QueueStatusResponseBuilder b)]) = _$QueueStatusResponse;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(QueueStatusResponseBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<QueueStatusResponse> get serializer => _$QueueStatusResponseSerializer();
}

class _$QueueStatusResponseSerializer implements PrimitiveSerializer<QueueStatusResponse> {
  @override
  final Iterable<Type> types = const [QueueStatusResponse, _$QueueStatusResponse];

  @override
  final String wireName = r'QueueStatusResponse';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    QueueStatusResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'steering_depth';
    yield serializers.serialize(
      object.steeringDepth,
      specifiedType: const FullType(int),
    );
    yield r'followup_depth';
    yield serializers.serialize(
      object.followupDepth,
      specifiedType: const FullType(int),
    );
    yield r'is_active';
    yield serializers.serialize(
      object.isActive,
      specifiedType: const FullType(bool),
    );
    yield r'generation';
    yield serializers.serialize(
      object.generation,
      specifiedType: const FullType(int),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    QueueStatusResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required QueueStatusResponseBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'steering_depth':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.steeringDepth = valueDes;
          break;
        case r'followup_depth':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.followupDepth = valueDes;
          break;
        case r'is_active':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.isActive = valueDes;
          break;
        case r'generation':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.generation = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  QueueStatusResponse deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = QueueStatusResponseBuilder();
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

