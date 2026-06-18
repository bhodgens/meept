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
/// * [limitCommaOmitempty] 
/// * [categoryCommaOmitempty] 
@BuiltValue()
abstract class MemoryQueryRequest implements Built<MemoryQueryRequest, MemoryQueryRequestBuilder> {
  @BuiltValueField(wireName: r'query')
  String get query;

  @BuiltValueField(wireName: r'limit,omitempty')
  int? get limitCommaOmitempty;

  @BuiltValueField(wireName: r'category,omitempty')
  String? get categoryCommaOmitempty;

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
    if (object.limitCommaOmitempty != null) {
      yield r'limit,omitempty';
      yield serializers.serialize(
        object.limitCommaOmitempty,
        specifiedType: const FullType(int),
      );
    }
    if (object.categoryCommaOmitempty != null) {
      yield r'category,omitempty';
      yield serializers.serialize(
        object.categoryCommaOmitempty,
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
        case r'limit,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.limitCommaOmitempty = valueDes;
          break;
        case r'category,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.categoryCommaOmitempty = valueDes;
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

