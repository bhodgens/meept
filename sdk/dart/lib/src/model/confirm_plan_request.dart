//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'confirm_plan_request.g.dart';

/// ConfirmPlanRequest
///
/// Properties:
/// * [planId] 
/// * [sessionId] 
/// * [by] 
@BuiltValue()
abstract class ConfirmPlanRequest implements Built<ConfirmPlanRequest, ConfirmPlanRequestBuilder> {
  @BuiltValueField(wireName: r'plan_id')
  String get planId;

  @BuiltValueField(wireName: r'session_id')
  String get sessionId;

  @BuiltValueField(wireName: r'by')
  String get by;

  ConfirmPlanRequest._();

  factory ConfirmPlanRequest([void updates(ConfirmPlanRequestBuilder b)]) = _$ConfirmPlanRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ConfirmPlanRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ConfirmPlanRequest> get serializer => _$ConfirmPlanRequestSerializer();
}

class _$ConfirmPlanRequestSerializer implements PrimitiveSerializer<ConfirmPlanRequest> {
  @override
  final Iterable<Type> types = const [ConfirmPlanRequest, _$ConfirmPlanRequest];

  @override
  final String wireName = r'ConfirmPlanRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ConfirmPlanRequest object, {
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
    yield r'by';
    yield serializers.serialize(
      object.by,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    ConfirmPlanRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ConfirmPlanRequestBuilder result,
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
        case r'by':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.by = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ConfirmPlanRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ConfirmPlanRequestBuilder();
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

