//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'apply_improvement_request.g.dart';

/// ApplyImprovementRequest
///
/// Properties:
/// * [improvementId] 
@BuiltValue()
abstract class ApplyImprovementRequest implements Built<ApplyImprovementRequest, ApplyImprovementRequestBuilder> {
  @BuiltValueField(wireName: r'improvement_id')
  String get improvementId;

  ApplyImprovementRequest._();

  factory ApplyImprovementRequest([void updates(ApplyImprovementRequestBuilder b)]) = _$ApplyImprovementRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ApplyImprovementRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ApplyImprovementRequest> get serializer => _$ApplyImprovementRequestSerializer();
}

class _$ApplyImprovementRequestSerializer implements PrimitiveSerializer<ApplyImprovementRequest> {
  @override
  final Iterable<Type> types = const [ApplyImprovementRequest, _$ApplyImprovementRequest];

  @override
  final String wireName = r'ApplyImprovementRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ApplyImprovementRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'improvement_id';
    yield serializers.serialize(
      object.improvementId,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    ApplyImprovementRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ApplyImprovementRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'improvement_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.improvementId = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ApplyImprovementRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ApplyImprovementRequestBuilder();
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

