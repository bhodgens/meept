//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'revise_plan_request.g.dart';

/// RevisePlanRequest
///
/// Properties:
/// * [planId] 
/// * [sessionId] 
/// * [feedback] 
@BuiltValue()
abstract class RevisePlanRequest implements Built<RevisePlanRequest, RevisePlanRequestBuilder> {
  @BuiltValueField(wireName: r'plan_id')
  String get planId;

  @BuiltValueField(wireName: r'session_id')
  String get sessionId;

  @BuiltValueField(wireName: r'feedback')
  String get feedback;

  RevisePlanRequest._();

  factory RevisePlanRequest([void updates(RevisePlanRequestBuilder b)]) = _$RevisePlanRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(RevisePlanRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<RevisePlanRequest> get serializer => _$RevisePlanRequestSerializer();
}

class _$RevisePlanRequestSerializer implements PrimitiveSerializer<RevisePlanRequest> {
  @override
  final Iterable<Type> types = const [RevisePlanRequest, _$RevisePlanRequest];

  @override
  final String wireName = r'RevisePlanRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    RevisePlanRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'plan_id';
    yield serializers.serialize(
      object.planId,
      specifiedType: const FullType(String),
    );
    yield r'session_id';
    yield serializers.serialize(
      object.sessionId,
      specifiedType: const FullType(String),
    );
    yield r'feedback';
    yield serializers.serialize(
      object.feedback,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    RevisePlanRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required RevisePlanRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'plan_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.planId = valueDes;
          break;
        case r'session_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.sessionId = valueDes;
          break;
        case r'feedback':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.feedback = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  RevisePlanRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = RevisePlanRequestBuilder();
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

