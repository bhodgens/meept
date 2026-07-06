//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'search_request.g.dart';

/// SearchRequest
///
/// Properties:
/// * [query] 
/// * [scope] 
/// * [limit] 
@BuiltValue()
abstract class SearchRequest implements Built<SearchRequest, SearchRequestBuilder> {
  @BuiltValueField(wireName: r'query')
  String get query;

  @BuiltValueField(wireName: r'scope')
  String? get scope;

  @BuiltValueField(wireName: r'limit')
  int? get limit;

  SearchRequest._();

  factory SearchRequest([void updates(SearchRequestBuilder b)]) = _$SearchRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SearchRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SearchRequest> get serializer => _$SearchRequestSerializer();
}

class _$SearchRequestSerializer implements PrimitiveSerializer<SearchRequest> {
  @override
  final Iterable<Type> types = const [SearchRequest, _$SearchRequest];

  @override
  final String wireName = r'SearchRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SearchRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'query';
    yield serializers.serialize(
      object.query,
      specifiedType: const FullType(String),
    );
    if (object.scope != null) {
      yield r'scope';
      yield serializers.serialize(
        object.scope,
        specifiedType: const FullType(String),
      );
    }
    if (object.limit != null) {
      yield r'limit';
      yield serializers.serialize(
        object.limit,
        specifiedType: const FullType(int),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    SearchRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SearchRequestBuilder result,
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
        case r'scope':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.scope = valueDes;
          break;
        case r'limit':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.limit = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SearchRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SearchRequestBuilder();
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

