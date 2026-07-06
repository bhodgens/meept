//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'vector_search_request.g.dart';

/// VectorSearchRequest
///
/// Properties:
/// * [query] 
/// * [limit] 
/// * [shardTypes] 
@BuiltValue()
abstract class VectorSearchRequest implements Built<VectorSearchRequest, VectorSearchRequestBuilder> {
  @BuiltValueField(wireName: r'query')
  String get query;

  @BuiltValueField(wireName: r'limit')
  int? get limit;

  @BuiltValueField(wireName: r'shard_types')
  String? get shardTypes;

  VectorSearchRequest._();

  factory VectorSearchRequest([void updates(VectorSearchRequestBuilder b)]) = _$VectorSearchRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(VectorSearchRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<VectorSearchRequest> get serializer => _$VectorSearchRequestSerializer();
}

class _$VectorSearchRequestSerializer implements PrimitiveSerializer<VectorSearchRequest> {
  @override
  final Iterable<Type> types = const [VectorSearchRequest, _$VectorSearchRequest];

  @override
  final String wireName = r'VectorSearchRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    VectorSearchRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'query';
    yield serializers.serialize(
      object.query,
      specifiedType: const FullType(String),
    );
    if (object.limit != null) {
      yield r'limit';
      yield serializers.serialize(
        object.limit,
        specifiedType: const FullType(int),
      );
    }
    if (object.shardTypes != null) {
      yield r'shard_types';
      yield serializers.serialize(
        object.shardTypes,
        specifiedType: const FullType.nullable(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    VectorSearchRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required VectorSearchRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'query':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.query = valueDes;
          break;
        case r'limit':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.limit = valueDes;
          break;
        case r'shard_types':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.shardTypes = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  VectorSearchRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = VectorSearchRequestBuilder();
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

