//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'model_info.g.dart';

/// ModelInfo
///
/// Properties:
/// * [provider] 
/// * [model] 
/// * [fullName] 
/// * [baseUrl] 
/// * [contextLimit] 
/// * [maxOutput] 
/// * [capabilities] 
/// * [isDefault] 
/// * [inputCost] 
/// * [outputCost] 
@BuiltValue()
abstract class ModelInfo implements Built<ModelInfo, ModelInfoBuilder> {
  @BuiltValueField(wireName: r'provider')
  String get provider;

  @BuiltValueField(wireName: r'model')
  String get model;

  @BuiltValueField(wireName: r'full_name')
  String get fullName;

  @BuiltValueField(wireName: r'base_url')
  String get baseUrl;

  @BuiltValueField(wireName: r'context_limit')
  int get contextLimit;

  @BuiltValueField(wireName: r'max_output')
  int get maxOutput;

  @BuiltValueField(wireName: r'capabilities')
  String? get capabilities;

  @BuiltValueField(wireName: r'is_default')
  bool get isDefault;

  @BuiltValueField(wireName: r'input_cost')
  num get inputCost;

  @BuiltValueField(wireName: r'output_cost')
  num get outputCost;

  ModelInfo._();

  factory ModelInfo([void updates(ModelInfoBuilder b)]) = _$ModelInfo;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ModelInfoBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ModelInfo> get serializer => _$ModelInfoSerializer();
}

class _$ModelInfoSerializer implements PrimitiveSerializer<ModelInfo> {
  @override
  final Iterable<Type> types = const [ModelInfo, _$ModelInfo];

  @override
  final String wireName = r'ModelInfo';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ModelInfo object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'provider';
    yield serializers.serialize(
      object.provider,
      specifiedType: const FullType(String),
    );
    yield r'model';
    yield serializers.serialize(
      object.model,
      specifiedType: const FullType(String),
    );
    yield r'full_name';
    yield serializers.serialize(
      object.fullName,
      specifiedType: const FullType(String),
    );
    yield r'base_url';
    yield serializers.serialize(
      object.baseUrl,
      specifiedType: const FullType(String),
    );
    yield r'context_limit';
    yield serializers.serialize(
      object.contextLimit,
      specifiedType: const FullType(int),
    );
    yield r'max_output';
    yield serializers.serialize(
      object.maxOutput,
      specifiedType: const FullType(int),
    );
    yield r'capabilities';
    yield object.capabilities == null ? null : serializers.serialize(
      object.capabilities,
      specifiedType: const FullType.nullable(String),
    );
    yield r'is_default';
    yield serializers.serialize(
      object.isDefault,
      specifiedType: const FullType(bool),
    );
    yield r'input_cost';
    yield serializers.serialize(
      object.inputCost,
      specifiedType: const FullType(num),
    );
    yield r'output_cost';
    yield serializers.serialize(
      object.outputCost,
      specifiedType: const FullType(num),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    ModelInfo object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ModelInfoBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'provider':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.provider = valueDes;
          break;
        case r'model':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.model = valueDes;
          break;
        case r'full_name':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.fullName = valueDes;
          break;
        case r'base_url':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.baseUrl = valueDes;
          break;
        case r'context_limit':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.contextLimit = valueDes;
          break;
        case r'max_output':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.maxOutput = valueDes;
          break;
        case r'capabilities':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.capabilities = valueDes;
          break;
        case r'is_default':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.isDefault = valueDes;
          break;
        case r'input_cost':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.inputCost = valueDes;
          break;
        case r'output_cost':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.outputCost = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ModelInfo deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ModelInfoBuilder();
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

