//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'shell_job_config.g.dart';

/// ShellJobConfig
///
/// Properties:
/// * [command] 
/// * [args] 
/// * [workDir] 
/// * [env] 
/// * [timeoutSecs] 
/// * [captureOutput] 
@BuiltValue()
abstract class ShellJobConfig implements Built<ShellJobConfig, ShellJobConfigBuilder> {
  @BuiltValueField(wireName: r'command')
  String get command;

  @BuiltValueField(wireName: r'args')
  String? get args;

  @BuiltValueField(wireName: r'work_dir')
  String? get workDir;

  @BuiltValueField(wireName: r'env')
  String? get env;

  @BuiltValueField(wireName: r'timeout_secs')
  int? get timeoutSecs;

  @BuiltValueField(wireName: r'capture_output')
  bool get captureOutput;

  ShellJobConfig._();

  factory ShellJobConfig([void updates(ShellJobConfigBuilder b)]) = _$ShellJobConfig;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ShellJobConfigBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ShellJobConfig> get serializer => _$ShellJobConfigSerializer();
}

class _$ShellJobConfigSerializer implements PrimitiveSerializer<ShellJobConfig> {
  @override
  final Iterable<Type> types = const [ShellJobConfig, _$ShellJobConfig];

  @override
  final String wireName = r'ShellJobConfig';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ShellJobConfig object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'command';
    yield serializers.serialize(
      object.command,
      specifiedType: const FullType(String),
    );
    if (object.args != null) {
      yield r'args';
      yield serializers.serialize(
        object.args,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.workDir != null) {
      yield r'work_dir';
      yield serializers.serialize(
        object.workDir,
        specifiedType: const FullType(String),
      );
    }
    if (object.env != null) {
      yield r'env';
      yield serializers.serialize(
        object.env,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.timeoutSecs != null) {
      yield r'timeout_secs';
      yield serializers.serialize(
        object.timeoutSecs,
        specifiedType: const FullType(int),
      );
    }
    yield r'capture_output';
    yield serializers.serialize(
      object.captureOutput,
      specifiedType: const FullType(bool),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    ShellJobConfig object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ShellJobConfigBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'command':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.command = valueDes;
          break;
        case r'args':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.args = valueDes;
          break;
        case r'work_dir':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.workDir = valueDes;
          break;
        case r'env':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.env = valueDes;
          break;
        case r'timeout_secs':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.timeoutSecs = valueDes;
          break;
        case r'capture_output':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.captureOutput = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ShellJobConfig deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ShellJobConfigBuilder();
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

