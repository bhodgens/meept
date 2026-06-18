//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'skills_service.g.dart';

/// SkillsService
///
/// Properties:
/// * [registry] 
/// * [executor] 
@BuiltValue()
abstract class SkillsService implements Built<SkillsService, SkillsServiceBuilder> {
  @BuiltValueField(wireName: r'registry')
  JsonObject? get registry;

  @BuiltValueField(wireName: r'executor')
  JsonObject? get executor;

  SkillsService._();

  factory SkillsService([void updates(SkillsServiceBuilder b)]) = _$SkillsService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SkillsServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SkillsService> get serializer => _$SkillsServiceSerializer();
}

class _$SkillsServiceSerializer implements PrimitiveSerializer<SkillsService> {
  @override
  final Iterable<Type> types = const [SkillsService, _$SkillsService];

  @override
  final String wireName = r'SkillsService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SkillsService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.registry != null) {
      yield r'registry';
      yield serializers.serialize(
        object.registry,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.executor != null) {
      yield r'executor';
      yield serializers.serialize(
        object.executor,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    SkillsService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SkillsServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'registry':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.registry = valueDes;
          break;
        case r'executor':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.executor = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SkillsService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SkillsServiceBuilder();
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

