//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'cache_stats_response.g.dart';

/// CacheStatsResponse
///
/// Properties:
/// * [hits] 
/// * [misses] 
/// * [size] 
@BuiltValue()
abstract class CacheStatsResponse implements Built<CacheStatsResponse, CacheStatsResponseBuilder> {
  @BuiltValueField(wireName: r'hits')
  int get hits;

  @BuiltValueField(wireName: r'misses')
  int get misses;

  @BuiltValueField(wireName: r'size')
  int get size;

  CacheStatsResponse._();

  factory CacheStatsResponse([void updates(CacheStatsResponseBuilder b)]) = _$CacheStatsResponse;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CacheStatsResponseBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CacheStatsResponse> get serializer => _$CacheStatsResponseSerializer();
}

class _$CacheStatsResponseSerializer implements PrimitiveSerializer<CacheStatsResponse> {
  @override
  final Iterable<Type> types = const [CacheStatsResponse, _$CacheStatsResponse];

  @override
  final String wireName = r'CacheStatsResponse';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CacheStatsResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'hits';
    yield serializers.serialize(
      object.hits,
      specifiedType: const FullType(int),
    );
    yield r'misses';
    yield serializers.serialize(
      object.misses,
      specifiedType: const FullType(int),
    );
    yield r'size';
    yield serializers.serialize(
      object.size,
      specifiedType: const FullType(int),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    CacheStatsResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CacheStatsResponseBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'hits':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.hits = valueDes;
          break;
        case r'misses':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.misses = valueDes;
          break;
        case r'size':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.size = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  CacheStatsResponse deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CacheStatsResponseBuilder();
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

