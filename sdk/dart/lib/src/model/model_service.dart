//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'model_service.g.dart';

/// ModelService
///
/// Properties:
/// * [configPath] 
/// * [credStore] 
/// * [stateDir] 
@BuiltValue()
abstract class ModelService implements Built<ModelService, ModelServiceBuilder> {
  @BuiltValueField(wireName: r'configPath')
  String? get configPath;

  @BuiltValueField(wireName: r'credStore')
  JsonObject? get credStore;

  @BuiltValueField(wireName: r'stateDir')
  String? get stateDir;

  ModelService._();

  factory ModelService([void updates(ModelServiceBuilder b)]) = _$ModelService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ModelServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ModelService> get serializer => _$ModelServiceSerializer();
}

class _$ModelServiceSerializer implements PrimitiveSerializer<ModelService> {
  @override
  final Iterable<Type> types = const [ModelService, _$ModelService];

  @override
  final String wireName = r'ModelService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ModelService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.configPath != null) {
      yield r'configPath';
      yield serializers.serialize(
        object.configPath,
        specifiedType: const FullType(String),
      );
    }
    if (object.credStore != null) {
      yield r'credStore';
      yield serializers.serialize(
        object.credStore,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.stateDir != null) {
      yield r'stateDir';
      yield serializers.serialize(
        object.stateDir,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    ModelService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ModelServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'configPath':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.configPath = valueDes;
          break;
        case r'credStore':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.credStore = valueDes;
          break;
        case r'stateDir':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.stateDir = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ModelService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ModelServiceBuilder();
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

