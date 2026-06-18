//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'memory_service.g.dart';

/// MemoryService
///
/// Properties:
/// * [manager] 
@BuiltValue()
abstract class MemoryService implements Built<MemoryService, MemoryServiceBuilder> {
  @BuiltValueField(wireName: r'manager')
  JsonObject? get manager;

  MemoryService._();

  factory MemoryService([void updates(MemoryServiceBuilder b)]) = _$MemoryService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(MemoryServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<MemoryService> get serializer => _$MemoryServiceSerializer();
}

class _$MemoryServiceSerializer implements PrimitiveSerializer<MemoryService> {
  @override
  final Iterable<Type> types = const [MemoryService, _$MemoryService];

  @override
  final String wireName = r'MemoryService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    MemoryService object, {
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
    MemoryService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required MemoryServiceBuilder result,
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
  MemoryService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = MemoryServiceBuilder();
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

