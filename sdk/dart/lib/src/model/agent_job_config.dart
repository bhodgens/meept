//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'agent_job_config.g.dart';

/// AgentJobConfig
///
/// Properties:
/// * [prompt] 
/// * [context] 
/// * [model] 
/// * [maxTokens] 
/// * [temperature] 
@BuiltValue()
abstract class AgentJobConfig implements Built<AgentJobConfig, AgentJobConfigBuilder> {
  @BuiltValueField(wireName: r'prompt')
  String get prompt;

  @BuiltValueField(wireName: r'context')
  String? get context;

  @BuiltValueField(wireName: r'model')
  String? get model;

  @BuiltValueField(wireName: r'max_tokens')
  int? get maxTokens;

  @BuiltValueField(wireName: r'temperature')
  num? get temperature;

  AgentJobConfig._();

  factory AgentJobConfig([void updates(AgentJobConfigBuilder b)]) = _$AgentJobConfig;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(AgentJobConfigBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<AgentJobConfig> get serializer => _$AgentJobConfigSerializer();
}

class _$AgentJobConfigSerializer implements PrimitiveSerializer<AgentJobConfig> {
  @override
  final Iterable<Type> types = const [AgentJobConfig, _$AgentJobConfig];

  @override
  final String wireName = r'AgentJobConfig';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    AgentJobConfig object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'prompt';
    yield serializers.serialize(
      object.prompt,
      specifiedType: const FullType(String),
    );
    if (object.context != null) {
      yield r'context';
      yield serializers.serialize(
        object.context,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.model != null) {
      yield r'model';
      yield serializers.serialize(
        object.model,
        specifiedType: const FullType(String),
      );
    }
    if (object.maxTokens != null) {
      yield r'max_tokens';
      yield serializers.serialize(
        object.maxTokens,
        specifiedType: const FullType(int),
      );
    }
    if (object.temperature != null) {
      yield r'temperature';
      yield serializers.serialize(
        object.temperature,
        specifiedType: const FullType(num),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    AgentJobConfig object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required AgentJobConfigBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'prompt':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.prompt = valueDes;
          break;
        case r'context':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.context = valueDes;
          break;
        case r'model':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.model = valueDes;
          break;
        case r'max_tokens':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.maxTokens = valueDes;
          break;
        case r'temperature':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.temperature = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  AgentJobConfig deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = AgentJobConfigBuilder();
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

