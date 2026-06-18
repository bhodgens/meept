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
/// * [contextCommaOmitempty] 
/// * [modelCommaOmitempty] 
/// * [maxTokensCommaOmitempty] 
/// * [temperatureCommaOmitempty] 
@BuiltValue()
abstract class AgentJobConfig implements Built<AgentJobConfig, AgentJobConfigBuilder> {
  @BuiltValueField(wireName: r'prompt')
  String get prompt;

  @BuiltValueField(wireName: r'context,omitempty')
  String? get contextCommaOmitempty;

  @BuiltValueField(wireName: r'model,omitempty')
  String? get modelCommaOmitempty;

  @BuiltValueField(wireName: r'max_tokens,omitempty')
  int? get maxTokensCommaOmitempty;

  @BuiltValueField(wireName: r'temperature,omitempty')
  num? get temperatureCommaOmitempty;

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
    if (object.contextCommaOmitempty != null) {
      yield r'context,omitempty';
      yield serializers.serialize(
        object.contextCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.modelCommaOmitempty != null) {
      yield r'model,omitempty';
      yield serializers.serialize(
        object.modelCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.maxTokensCommaOmitempty != null) {
      yield r'max_tokens,omitempty';
      yield serializers.serialize(
        object.maxTokensCommaOmitempty,
        specifiedType: const FullType(int),
      );
    }
    if (object.temperatureCommaOmitempty != null) {
      yield r'temperature,omitempty';
      yield serializers.serialize(
        object.temperatureCommaOmitempty,
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
        case r'context,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.contextCommaOmitempty = valueDes;
          break;
        case r'model,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.modelCommaOmitempty = valueDes;
          break;
        case r'max_tokens,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.maxTokensCommaOmitempty = valueDes;
          break;
        case r'temperature,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(num),
          ) as num;
          result.temperatureCommaOmitempty = valueDes;
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

