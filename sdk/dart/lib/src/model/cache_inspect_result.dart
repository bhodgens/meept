//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'cache_inspect_result.g.dart';

/// CacheInspectResult
///
/// Properties:
/// * [promptHash] 
/// * [modelId] 
/// * [createdAt] 
/// * [expiresAt] 
/// * [hitCount] 
/// * [fileHashesCommaOmitempty] 
/// * [source_] 
@BuiltValue()
abstract class CacheInspectResult implements Built<CacheInspectResult, CacheInspectResultBuilder> {
  @BuiltValueField(wireName: r'prompt_hash')
  String get promptHash;

  @BuiltValueField(wireName: r'model_id')
  String get modelId;

  @BuiltValueField(wireName: r'created_at')
  String get createdAt;

  @BuiltValueField(wireName: r'expires_at')
  String get expiresAt;

  @BuiltValueField(wireName: r'hit_count')
  int get hitCount;

  @BuiltValueField(wireName: r'file_hashes,omitempty')
  String? get fileHashesCommaOmitempty;

  @BuiltValueField(wireName: r'source')
  String get source_;

  CacheInspectResult._();

  factory CacheInspectResult([void updates(CacheInspectResultBuilder b)]) = _$CacheInspectResult;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CacheInspectResultBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CacheInspectResult> get serializer => _$CacheInspectResultSerializer();
}

class _$CacheInspectResultSerializer implements PrimitiveSerializer<CacheInspectResult> {
  @override
  final Iterable<Type> types = const [CacheInspectResult, _$CacheInspectResult];

  @override
  final String wireName = r'CacheInspectResult';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CacheInspectResult object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'prompt_hash';
    yield serializers.serialize(
      object.promptHash,
      specifiedType: const FullType(String),
    );
    yield r'model_id';
    yield serializers.serialize(
      object.modelId,
      specifiedType: const FullType(String),
    );
    yield r'created_at';
    yield serializers.serialize(
      object.createdAt,
      specifiedType: const FullType(String),
    );
    yield r'expires_at';
    yield serializers.serialize(
      object.expiresAt,
      specifiedType: const FullType(String),
    );
    yield r'hit_count';
    yield serializers.serialize(
      object.hitCount,
      specifiedType: const FullType(int),
    );
    if (object.fileHashesCommaOmitempty != null) {
      yield r'file_hashes,omitempty';
      yield serializers.serialize(
        object.fileHashesCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
    yield r'source';
    yield serializers.serialize(
      object.source_,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    CacheInspectResult object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CacheInspectResultBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'prompt_hash':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.promptHash = valueDes;
          break;
        case r'model_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.modelId = valueDes;
          break;
        case r'created_at':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.createdAt = valueDes;
          break;
        case r'expires_at':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.expiresAt = valueDes;
          break;
        case r'hit_count':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.hitCount = valueDes;
          break;
        case r'file_hashes,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.fileHashesCommaOmitempty = valueDes;
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
  CacheInspectResult deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CacheInspectResultBuilder();
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

