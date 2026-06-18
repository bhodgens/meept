//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'plan_service.g.dart';

/// PlanService
///
/// Properties:
/// * [manager] 
/// * [store] 
@BuiltValue()
abstract class PlanService implements Built<PlanService, PlanServiceBuilder> {
  @BuiltValueField(wireName: r'manager')
  JsonObject? get manager;

  @BuiltValueField(wireName: r'store')
  JsonObject? get store;

  PlanService._();

  factory PlanService([void updates(PlanServiceBuilder b)]) = _$PlanService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(PlanServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<PlanService> get serializer => _$PlanServiceSerializer();
}

class _$PlanServiceSerializer implements PrimitiveSerializer<PlanService> {
  @override
  final Iterable<Type> types = const [PlanService, _$PlanService];

  @override
  final String wireName = r'PlanService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    PlanService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.manager != null) {
      yield r'manager';
      yield serializers.serialize(
        object.manager,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.store != null) {
      yield r'store';
      yield serializers.serialize(
        object.store,
        specifiedType: const FullType(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    PlanService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required PlanServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'manager':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.manager = valueDes;
          break;
        case r'store':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.store = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  PlanService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = PlanServiceBuilder();
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

