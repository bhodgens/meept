//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'search_result.g.dart';

/// SearchResult
///
/// Properties:
/// * [type] 
/// * [id] 
/// * [title] 
/// * [snippet] 
/// * [relevance] 
@BuiltValue()
abstract class SearchResult implements Built<SearchResult, SearchResultBuilder> {
  @BuiltValueField(wireName: r'type')
  String get type;

  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'title')
  String get title;

  @BuiltValueField(wireName: r'snippet')
  String get snippet;

  @BuiltValueField(wireName: r'relevance')
  num get relevance;

  SearchResult._();

  factory SearchResult([void updates(SearchResultBuilder b)]) = _$SearchResult;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SearchResultBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SearchResult> get serializer => _$SearchResultSerializer();
}

class _$SearchResultSerializer implements PrimitiveSerializer<SearchResult> {
  @override
  final Iterable<Type> types = const [SearchResult, _$SearchResult];

  @override
  final String wireName = r'SearchResult';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SearchResult object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'type';
    yield serializers.serialize(
      object.type,
      specifiedType: const FullType(String),
    );
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    yield r'title';
    yield serializers.serialize(
      object.title,
      specifiedType: const FullType(String),
    );
    yield r'snippet';
    yield serializers.serialize(
      object.snippet,
      specifiedType: const FullType(String),
    );
    yield r'relevance';
    yield serializers.serialize(
      object.relevance,
      specifiedType: const FullType(num),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    SearchResult object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SearchResultBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'type':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.type = valueDes;
          break;
        case r'id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.id = valueDes;
          break;
        case r'title':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.title = valueDes;
          break;
        case r'snippet':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.snippet = valueDes;
          break;
        case r'relevance':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.relevance = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SearchResult deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SearchResultBuilder();
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

