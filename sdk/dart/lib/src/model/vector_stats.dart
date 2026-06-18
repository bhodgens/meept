//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'vector_stats.g.dart';

/// VectorStats
///
/// Properties:
/// * [loadedShards] 
/// * [maxRamShards] 
/// * [lruHits] 
/// * [lruMisses] 
/// * [lruEvictions] 
/// * [shardDetails] 
@BuiltValue()
abstract class VectorStats implements Built<VectorStats, VectorStatsBuilder> {
  @BuiltValueField(wireName: r'loaded_shards')
  int get loadedShards;

  @BuiltValueField(wireName: r'max_ram_shards')
  int get maxRamShards;

  @BuiltValueField(wireName: r'lru_hits')
  int get lruHits;

  @BuiltValueField(wireName: r'lru_misses')
  int get lruMisses;

  @BuiltValueField(wireName: r'lru_evictions')
  int get lruEvictions;

  @BuiltValueField(wireName: r'shard_details')
  String? get shardDetails;

  VectorStats._();

  factory VectorStats([void updates(VectorStatsBuilder b)]) = _$VectorStats;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(VectorStatsBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<VectorStats> get serializer => _$VectorStatsSerializer();
}

class _$VectorStatsSerializer implements PrimitiveSerializer<VectorStats> {
  @override
  final Iterable<Type> types = const [VectorStats, _$VectorStats];

  @override
  final String wireName = r'VectorStats';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    VectorStats object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'loaded_shards';
    yield serializers.serialize(
      object.loadedShards,
      specifiedType: const FullType(int),
    );
    yield r'max_ram_shards';
    yield serializers.serialize(
      object.maxRamShards,
      specifiedType: const FullType(int),
    );
    yield r'lru_hits';
    yield serializers.serialize(
      object.lruHits,
      specifiedType: const FullType(int),
    );
    yield r'lru_misses';
    yield serializers.serialize(
      object.lruMisses,
      specifiedType: const FullType(int),
    );
    yield r'lru_evictions';
    yield serializers.serialize(
      object.lruEvictions,
      specifiedType: const FullType(int),
    );
    yield r'shard_details';
    yield object.shardDetails == null ? null : serializers.serialize(
      object.shardDetails,
      specifiedType: const FullType.nullable(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    VectorStats object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required VectorStatsBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'loaded_shards':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.loadedShards = valueDes;
          break;
        case r'max_ram_shards':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.maxRamShards = valueDes;
          break;
        case r'lru_hits':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.lruHits = valueDes;
          break;
        case r'lru_misses':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.lruMisses = valueDes;
          break;
        case r'lru_evictions':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.lruEvictions = valueDes;
          break;
        case r'shard_details':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.shardDetails = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  VectorStats deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = VectorStatsBuilder();
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

