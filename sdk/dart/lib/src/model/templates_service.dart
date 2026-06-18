//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'templates_service.g.dart';

/// TemplatesService
///
/// Properties:
/// * [registry] 
/// * [executor] 
@BuiltValue()
abstract class TemplatesService implements Built<TemplatesService, TemplatesServiceBuilder> {
  @BuiltValueField(wireName: r'registry')
  JsonObject? get registry;

  @BuiltValueField(wireName: r'executor')
  JsonObject? get executor;

  TemplatesService._();

  factory TemplatesService([void updates(TemplatesServiceBuilder b)]) = _$TemplatesService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(TemplatesServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<TemplatesService> get serializer => _$TemplatesServiceSerializer();
}

class _$TemplatesServiceSerializer implements PrimitiveSerializer<TemplatesService> {
  @override
  final Iterable<Type> types = const [TemplatesService, _$TemplatesService];

  @override
  final String wireName = r'TemplatesService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    TemplatesService object, {
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
    TemplatesService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required TemplatesServiceBuilder result,
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
  TemplatesService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = TemplatesServiceBuilder();
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

