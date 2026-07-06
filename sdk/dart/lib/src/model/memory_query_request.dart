//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'memory_query_request.g.dart';

/// MemoryQueryRequest
///
/// Properties:
/// * [query] 
/// * [limit] 
/// * [category] 
@BuiltValue()
abstract class MemoryQueryRequest implements Built<MemoryQueryRequest, MemoryQueryRequestBuilder> {
  @BuiltValueField(wireName: r'query')
  String get query;

  @BuiltValueField(wireName: r'limit')
  int? get limit;

  @BuiltValueField(wireName: r'category')
  String? get category;

  MemoryQueryRequest._();

  factory MemoryQueryRequest([void updates(MemoryQueryRequestBuilder b)]) = _$MemoryQueryRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(MemoryQueryRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<MemoryQueryRequest> get serializer => _$MemoryQueryRequestSerializer();
}

class _$MemoryQueryRequestSerializer implements PrimitiveSerializer<MemoryQueryRequest> {
  @override
  final Iterable<Type> types = const [MemoryQueryRequest, _$MemoryQueryRequest];

  @override
  final String wireName = r'MemoryQueryRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    MemoryQueryRequest object, {
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
    if (object.category != null) {
      yield r'category';
      yield serializers.serialize(
        object.category,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    MemoryQueryRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required MemoryQueryRequestBuilder result,
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
        case r'category':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.category = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  MemoryQueryRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = MemoryQueryRequestBuilder();
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

