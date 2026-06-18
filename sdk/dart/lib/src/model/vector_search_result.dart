//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'vector_search_result.g.dart';

/// VectorSearchResult
///
/// Properties:
/// * [memoryId] 
/// * [content] 
/// * [metadataCommaOmitempty] 
/// * [relevanceScore] 
/// * [vectorSimilarity] 
@BuiltValue()
abstract class VectorSearchResult implements Built<VectorSearchResult, VectorSearchResultBuilder> {
  @BuiltValueField(wireName: r'memory_id')
  String get memoryId;

  @BuiltValueField(wireName: r'content')
  String get content;

  @BuiltValueField(wireName: r'metadata,omitempty')
  String? get metadataCommaOmitempty;

  @BuiltValueField(wireName: r'relevance_score')
  num get relevanceScore;

  @BuiltValueField(wireName: r'vector_similarity')
  num get vectorSimilarity;

  VectorSearchResult._();

  factory VectorSearchResult([void updates(VectorSearchResultBuilder b)]) = _$VectorSearchResult;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(VectorSearchResultBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<VectorSearchResult> get serializer => _$VectorSearchResultSerializer();
}

class _$VectorSearchResultSerializer implements PrimitiveSerializer<VectorSearchResult> {
  @override
  final Iterable<Type> types = const [VectorSearchResult, _$VectorSearchResult];

  @override
  final String wireName = r'VectorSearchResult';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    VectorSearchResult object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'memory_id';
    yield serializers.serialize(
      object.memoryId,
      specifiedType: const FullType(String),
    );
    yield r'content';
    yield serializers.serialize(
      object.content,
      specifiedType: const FullType(String),
    );
    if (object.metadataCommaOmitempty != null) {
      yield r'metadata,omitempty';
      yield serializers.serialize(
        object.metadataCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
    yield r'relevance_score';
    yield serializers.serialize(
      object.relevanceScore,
      specifiedType: const FullType(num),
    );
    yield r'vector_similarity';
    yield serializers.serialize(
      object.vectorSimilarity,
      specifiedType: const FullType(num),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    VectorSearchResult object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required VectorSearchResultBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'memory_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.memoryId = valueDes;
          break;
        case r'content':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.content = valueDes;
          break;
        case r'metadata,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.metadataCommaOmitempty = valueDes;
          break;
        case r'relevance_score':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.relevanceScore = valueDes;
          break;
        case r'vector_similarity':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.vectorSimilarity = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  VectorSearchResult deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = VectorSearchResultBuilder();
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

