//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'generate_improvement_request.g.dart';

/// GenerateImprovementRequest
///
/// Properties:
/// * [improvementId] 
@BuiltValue()
abstract class GenerateImprovementRequest implements Built<GenerateImprovementRequest, GenerateImprovementRequestBuilder> {
  @BuiltValueField(wireName: r'improvement_id')
  String get improvementId;

  GenerateImprovementRequest._();

  factory GenerateImprovementRequest([void updates(GenerateImprovementRequestBuilder b)]) = _$GenerateImprovementRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(GenerateImprovementRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<GenerateImprovementRequest> get serializer => _$GenerateImprovementRequestSerializer();
}

class _$GenerateImprovementRequestSerializer implements PrimitiveSerializer<GenerateImprovementRequest> {
  @override
  final Iterable<Type> types = const [GenerateImprovementRequest, _$GenerateImprovementRequest];

  @override
  final String wireName = r'GenerateImprovementRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    GenerateImprovementRequest object, {
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
    GenerateImprovementRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required GenerateImprovementRequestBuilder result,
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
  GenerateImprovementRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = GenerateImprovementRequestBuilder();
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

