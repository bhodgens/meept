//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'reject_plan_request.g.dart';

/// RejectPlanRequest
///
/// Properties:
/// * [planId] 
/// * [sessionId] 
/// * [by] 
/// * [reasonCommaOmitempty] 
@BuiltValue()
abstract class RejectPlanRequest implements Built<RejectPlanRequest, RejectPlanRequestBuilder> {
  @BuiltValueField(wireName: r'plan_id')
  String get planId;

  @BuiltValueField(wireName: r'session_id')
  String get sessionId;

  @BuiltValueField(wireName: r'by')
  String get by;

  @BuiltValueField(wireName: r'reason,omitempty')
  String? get reasonCommaOmitempty;

  RejectPlanRequest._();

  factory RejectPlanRequest([void updates(RejectPlanRequestBuilder b)]) = _$RejectPlanRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(RejectPlanRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<RejectPlanRequest> get serializer => _$RejectPlanRequestSerializer();
}

class _$RejectPlanRequestSerializer implements PrimitiveSerializer<RejectPlanRequest> {
  @override
  final Iterable<Type> types = const [RejectPlanRequest, _$RejectPlanRequest];

  @override
  final String wireName = r'RejectPlanRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    RejectPlanRequest object, {
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
    if (object.reasonCommaOmitempty != null) {
      yield r'reason,omitempty';
      yield serializers.serialize(
        object.reasonCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    RejectPlanRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required RejectPlanRequestBuilder result,
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
        case r'reason,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.reasonCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  RejectPlanRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = RejectPlanRequestBuilder();
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

