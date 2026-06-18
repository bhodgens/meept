//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'validate_improvement_request.g.dart';

/// ValidateImprovementRequest
///
/// Properties:
/// * [improvementId] 
@BuiltValue()
abstract class ValidateImprovementRequest implements Built<ValidateImprovementRequest, ValidateImprovementRequestBuilder> {
  @BuiltValueField(wireName: r'improvement_id')
  String get improvementId;

  ValidateImprovementRequest._();

  factory ValidateImprovementRequest([void updates(ValidateImprovementRequestBuilder b)]) = _$ValidateImprovementRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ValidateImprovementRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ValidateImprovementRequest> get serializer => _$ValidateImprovementRequestSerializer();
}

class _$ValidateImprovementRequestSerializer implements PrimitiveSerializer<ValidateImprovementRequest> {
  @override
  final Iterable<Type> types = const [ValidateImprovementRequest, _$ValidateImprovementRequest];

  @override
  final String wireName = r'ValidateImprovementRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ValidateImprovementRequest object, {
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
    ValidateImprovementRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ValidateImprovementRequestBuilder result,
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
  ValidateImprovementRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ValidateImprovementRequestBuilder();
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

