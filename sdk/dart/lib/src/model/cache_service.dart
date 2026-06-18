//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'cache_service.g.dart';

/// CacheService
///
/// Properties:
/// * [cache] 
@BuiltValue()
abstract class CacheService implements Built<CacheService, CacheServiceBuilder> {
  @BuiltValueField(wireName: r'cache')
  JsonObject? get cache;

  CacheService._();

  factory CacheService([void updates(CacheServiceBuilder b)]) = _$CacheService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CacheServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CacheService> get serializer => _$CacheServiceSerializer();
}

class _$CacheServiceSerializer implements PrimitiveSerializer<CacheService> {
  @override
  final Iterable<Type> types = const [CacheService, _$CacheService];

  @override
  final String wireName = r'CacheService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CacheService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.cache != null) {
      yield r'cache';
      yield serializers.serialize(
        object.cache,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    CacheService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CacheServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'cache':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.cache = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  CacheService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CacheServiceBuilder();
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

