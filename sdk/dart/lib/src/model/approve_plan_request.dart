//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'approve_plan_request.g.dart';

/// ApprovePlanRequest
///
/// Properties:
/// * [planId] 
/// * [sessionId] 
/// * [by] 
@BuiltValue()
abstract class ApprovePlanRequest implements Built<ApprovePlanRequest, ApprovePlanRequestBuilder> {
  @BuiltValueField(wireName: r'plan_id')
  String get planId;

  @BuiltValueField(wireName: r'session_id')
  String get sessionId;

  @BuiltValueField(wireName: r'by')
  String get by;

  ApprovePlanRequest._();

  factory ApprovePlanRequest([void updates(ApprovePlanRequestBuilder b)]) = _$ApprovePlanRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ApprovePlanRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ApprovePlanRequest> get serializer => _$ApprovePlanRequestSerializer();
}

class _$ApprovePlanRequestSerializer implements PrimitiveSerializer<ApprovePlanRequest> {
  @override
  final Iterable<Type> types = const [ApprovePlanRequest, _$ApprovePlanRequest];

  @override
  final String wireName = r'ApprovePlanRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ApprovePlanRequest object, {
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
    ApprovePlanRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ApprovePlanRequestBuilder result,
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
  ApprovePlanRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ApprovePlanRequestBuilder();
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

