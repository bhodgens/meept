//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'runtime_service.g.dart';

/// RuntimeService
///
/// Properties:
/// * [manager] 
@BuiltValue()
abstract class RuntimeService implements Built<RuntimeService, RuntimeServiceBuilder> {
  @BuiltValueField(wireName: r'manager')
  JsonObject? get manager;

  RuntimeService._();

  factory RuntimeService([void updates(RuntimeServiceBuilder b)]) = _$RuntimeService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(RuntimeServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<RuntimeService> get serializer => _$RuntimeServiceSerializer();
}

class _$RuntimeServiceSerializer implements PrimitiveSerializer<RuntimeService> {
  @override
  final Iterable<Type> types = const [RuntimeService, _$RuntimeService];

  @override
  final String wireName = r'RuntimeService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    RuntimeService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.manager != null) {
      yield r'manager';
      yield serializers.serialize(
        object.manager,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    RuntimeService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required RuntimeServiceBuilder result,
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
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  RuntimeService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = RuntimeServiceBuilder();
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

