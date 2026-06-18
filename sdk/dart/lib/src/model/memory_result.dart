//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'memory_result.g.dart';

/// MemoryResult
///
/// Properties:
/// * [memory] 
/// * [relevanceScore] 
/// * [source_] 
@BuiltValue()
abstract class MemoryResult implements Built<MemoryResult, MemoryResultBuilder> {
  @BuiltValueField(wireName: r'memory')
  JsonObject get memory;

  @BuiltValueField(wireName: r'relevance_score')
  num get relevanceScore;

  @BuiltValueField(wireName: r'source')
  String get source_;

  MemoryResult._();

  factory MemoryResult([void updates(MemoryResultBuilder b)]) = _$MemoryResult;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(MemoryResultBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<MemoryResult> get serializer => _$MemoryResultSerializer();
}

class _$MemoryResultSerializer implements PrimitiveSerializer<MemoryResult> {
  @override
  final Iterable<Type> types = const [MemoryResult, _$MemoryResult];

  @override
  final String wireName = r'MemoryResult';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    MemoryResult object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'memory';
    yield serializers.serialize(
      object.memory,
      specifiedType: const FullType(JsonObject),
    );
    yield r'relevance_score';
    yield serializers.serialize(
      object.relevanceScore,
      specifiedType: const FullType(num),
    );
    yield r'source';
    yield serializers.serialize(
      object.source_,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    MemoryResult object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required MemoryResultBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'memory':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.memory = valueDes;
          break;
        case r'relevance_score':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.relevanceScore = valueDes;
          break;
        case r'source':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.source_ = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  MemoryResult deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = MemoryResultBuilder();
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

