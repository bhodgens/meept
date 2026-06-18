//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'search_service.g.dart';

/// SearchService
///
/// Properties:
/// * [sessionStore] 
/// * [taskRegistry] 
/// * [memoryMgr] 
/// * [planStore] 
@BuiltValue()
abstract class SearchService implements Built<SearchService, SearchServiceBuilder> {
  @BuiltValueField(wireName: r'sessionStore')
  JsonObject? get sessionStore;

  @BuiltValueField(wireName: r'taskRegistry')
  JsonObject? get taskRegistry;

  @BuiltValueField(wireName: r'memoryMgr')
  JsonObject? get memoryMgr;

  @BuiltValueField(wireName: r'planStore')
  JsonObject? get planStore;

  SearchService._();

  factory SearchService([void updates(SearchServiceBuilder b)]) = _$SearchService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SearchServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SearchService> get serializer => _$SearchServiceSerializer();
}

class _$SearchServiceSerializer implements PrimitiveSerializer<SearchService> {
  @override
  final Iterable<Type> types = const [SearchService, _$SearchService];

  @override
  final String wireName = r'SearchService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SearchService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.sessionStore != null) {
      yield r'sessionStore';
      yield serializers.serialize(
        object.sessionStore,
        specifiedType: const FullType(JsonObject),
      );
    }
    if (object.taskRegistry != null) {
      yield r'taskRegistry';
      yield serializers.serialize(
        object.taskRegistry,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.memoryMgr != null) {
      yield r'memoryMgr';
      yield serializers.serialize(
        object.memoryMgr,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.planStore != null) {
      yield r'planStore';
      yield serializers.serialize(
        object.planStore,
        specifiedType: const FullType(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    SearchService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SearchServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'sessionStore':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.sessionStore = valueDes;
          break;
        case r'taskRegistry':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.taskRegistry = valueDes;
          break;
        case r'memoryMgr':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.memoryMgr = valueDes;
          break;
        case r'planStore':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.planStore = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SearchService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SearchServiceBuilder();
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

