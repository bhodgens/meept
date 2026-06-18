//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'shard_detail.g.dart';

/// ShardDetail
///
/// Properties:
/// * [dimension] 
/// * [m] 
/// * [efConstruction] 
/// * [efSearch] 
/// * [vectorCount] 
/// * [databaseSizeBytes] 
/// * [shardId] 
@BuiltValue()
abstract class ShardDetail implements Built<ShardDetail, ShardDetailBuilder> {
  @BuiltValueField(wireName: r'dimension')
  int get dimension;

  @BuiltValueField(wireName: r'm')
  int get m;

  @BuiltValueField(wireName: r'ef_construction')
  int get efConstruction;

  @BuiltValueField(wireName: r'ef_search')
  int get efSearch;

  @BuiltValueField(wireName: r'vector_count')
  int get vectorCount;

  @BuiltValueField(wireName: r'database_size_bytes')
  int get databaseSizeBytes;

  @BuiltValueField(wireName: r'shard_id')
  String get shardId;

  ShardDetail._();

  factory ShardDetail([void updates(ShardDetailBuilder b)]) = _$ShardDetail;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ShardDetailBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ShardDetail> get serializer => _$ShardDetailSerializer();
}

class _$ShardDetailSerializer implements PrimitiveSerializer<ShardDetail> {
  @override
  final Iterable<Type> types = const [ShardDetail, _$ShardDetail];

  @override
  final String wireName = r'ShardDetail';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ShardDetail object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'dimension';
    yield serializers.serialize(
      object.dimension,
      specifiedType: const FullType(int),
    );
    yield r'm';
    yield serializers.serialize(
      object.m,
      specifiedType: const FullType(int),
    );
    yield r'ef_construction';
    yield serializers.serialize(
      object.efConstruction,
      specifiedType: const FullType(int),
    );
    yield r'ef_search';
    yield serializers.serialize(
      object.efSearch,
      specifiedType: const FullType(int),
    );
    yield r'vector_count';
    yield serializers.serialize(
      object.vectorCount,
      specifiedType: const FullType(int),
    );
    yield r'database_size_bytes';
    yield serializers.serialize(
      object.databaseSizeBytes,
      specifiedType: const FullType(int),
    );
    yield r'shard_id';
    yield serializers.serialize(
      object.shardId,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    ShardDetail object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ShardDetailBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'dimension':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.dimension = valueDes;
          break;
        case r'm':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.m = valueDes;
          break;
        case r'ef_construction':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.efConstruction = valueDes;
          break;
        case r'ef_search':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.efSearch = valueDes;
          break;
        case r'vector_count':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.vectorCount = valueDes;
          break;
        case r'database_size_bytes':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.databaseSizeBytes = valueDes;
          break;
        case r'shard_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.shardId = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ShardDetail deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ShardDetailBuilder();
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

