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
/// * [argsCommaOmitempty] 
/// * [workDirCommaOmitempty] 
/// * [envCommaOmitempty] 
/// * [timeoutSecsCommaOmitempty] 
/// * [captureOutput] 
@BuiltValue()
abstract class ShellJobConfig implements Built<ShellJobConfig, ShellJobConfigBuilder> {
  @BuiltValueField(wireName: r'command')
  String get command;

  @BuiltValueField(wireName: r'args,omitempty')
  String? get argsCommaOmitempty;

  @BuiltValueField(wireName: r'work_dir,omitempty')
  String? get workDirCommaOmitempty;

  @BuiltValueField(wireName: r'env,omitempty')
  String? get envCommaOmitempty;

  @BuiltValueField(wireName: r'timeout_secs,omitempty')
  int? get timeoutSecsCommaOmitempty;

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
    if (object.argsCommaOmitempty != null) {
      yield r'args,omitempty';
      yield serializers.serialize(
        object.argsCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.workDirCommaOmitempty != null) {
      yield r'work_dir,omitempty';
      yield serializers.serialize(
        object.workDirCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.envCommaOmitempty != null) {
      yield r'env,omitempty';
      yield serializers.serialize(
        object.envCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.timeoutSecsCommaOmitempty != null) {
      yield r'timeout_secs,omitempty';
      yield serializers.serialize(
        object.timeoutSecsCommaOmitempty,
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
        case r'args,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.argsCommaOmitempty = valueDes;
          break;
        case r'work_dir,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.workDirCommaOmitempty = valueDes;
          break;
        case r'env,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.envCommaOmitempty = valueDes;
          break;
        case r'timeout_secs,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.timeoutSecsCommaOmitempty = valueDes;
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

