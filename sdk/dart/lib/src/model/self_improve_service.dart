//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'self_improve_service.g.dart';

/// SelfImproveService
///
/// Properties:
/// * [controller] 
@BuiltValue()
abstract class SelfImproveService implements Built<SelfImproveService, SelfImproveServiceBuilder> {
  @BuiltValueField(wireName: r'controller')
  JsonObject? get controller;

  SelfImproveService._();

  factory SelfImproveService([void updates(SelfImproveServiceBuilder b)]) = _$SelfImproveService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SelfImproveServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SelfImproveService> get serializer => _$SelfImproveServiceSerializer();
}

class _$SelfImproveServiceSerializer implements PrimitiveSerializer<SelfImproveService> {
  @override
  final Iterable<Type> types = const [SelfImproveService, _$SelfImproveService];

  @override
  final String wireName = r'SelfImproveService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SelfImproveService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.controller != null) {
      yield r'controller';
      yield serializers.serialize(
        object.controller,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    SelfImproveService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SelfImproveServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'controller':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.controller = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SelfImproveService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SelfImproveServiceBuilder();
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

