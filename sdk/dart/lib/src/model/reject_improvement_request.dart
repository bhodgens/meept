//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'reject_improvement_request.g.dart';

/// RejectImprovementRequest
///
/// Properties:
/// * [improvementId] 
/// * [reason] 
@BuiltValue()
abstract class RejectImprovementRequest implements Built<RejectImprovementRequest, RejectImprovementRequestBuilder> {
  @BuiltValueField(wireName: r'improvement_id')
  String get improvementId;

  @BuiltValueField(wireName: r'reason')
  String get reason;

  RejectImprovementRequest._();

  factory RejectImprovementRequest([void updates(RejectImprovementRequestBuilder b)]) = _$RejectImprovementRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(RejectImprovementRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<RejectImprovementRequest> get serializer => _$RejectImprovementRequestSerializer();
}

class _$RejectImprovementRequestSerializer implements PrimitiveSerializer<RejectImprovementRequest> {
  @override
  final Iterable<Type> types = const [RejectImprovementRequest, _$RejectImprovementRequest];

  @override
  final String wireName = r'RejectImprovementRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    RejectImprovementRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'improvement_id';
    yield serializers.serialize(
      object.improvementId,
      specifiedType: const FullType(String),
    );
    yield r'reason';
    yield serializers.serialize(
      object.reason,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    RejectImprovementRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required RejectImprovementRequestBuilder result,
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
        case r'reason':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.reason = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  RejectImprovementRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = RejectImprovementRequestBuilder();
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

