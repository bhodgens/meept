//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'scale_workers_request.g.dart';

/// ScaleWorkersRequest
///
/// Properties:
/// * [desiredCount] 
@BuiltValue()
abstract class ScaleWorkersRequest implements Built<ScaleWorkersRequest, ScaleWorkersRequestBuilder> {
  @BuiltValueField(wireName: r'desired_count')
  int get desiredCount;

  ScaleWorkersRequest._();

  factory ScaleWorkersRequest([void updates(ScaleWorkersRequestBuilder b)]) = _$ScaleWorkersRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ScaleWorkersRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ScaleWorkersRequest> get serializer => _$ScaleWorkersRequestSerializer();
}

class _$ScaleWorkersRequestSerializer implements PrimitiveSerializer<ScaleWorkersRequest> {
  @override
  final Iterable<Type> types = const [ScaleWorkersRequest, _$ScaleWorkersRequest];

  @override
  final String wireName = r'ScaleWorkersRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ScaleWorkersRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'desired_count';
    yield serializers.serialize(
      object.desiredCount,
      specifiedType: const FullType(int),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    ScaleWorkersRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ScaleWorkersRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'desired_count':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.desiredCount = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ScaleWorkersRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ScaleWorkersRequestBuilder();
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

