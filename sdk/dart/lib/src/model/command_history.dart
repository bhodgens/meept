//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'command_history.g.dart';

/// CommandHistory
///
/// Properties:
/// * [id] 
/// * [command] 
/// * [outputCommaOmitempty] 
/// * [stderrCommaOmitempty] 
/// * [exitCode] 
/// * [timestamp] 
/// * [workingDir] 
/// * [durationMs] 
/// * [riskLevel] 
/// * [success] 
@BuiltValue()
abstract class CommandHistory implements Built<CommandHistory, CommandHistoryBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'command')
  String get command;

  @BuiltValueField(wireName: r'output,omitempty')
  String? get outputCommaOmitempty;

  @BuiltValueField(wireName: r'stderr,omitempty')
  String? get stderrCommaOmitempty;

  @BuiltValueField(wireName: r'exit_code')
  int get exitCode;

  @BuiltValueField(wireName: r'timestamp')
  String get timestamp;

  @BuiltValueField(wireName: r'working_dir')
  String get workingDir;

  @BuiltValueField(wireName: r'duration_ms')
  JsonObject get durationMs;

  @BuiltValueField(wireName: r'risk_level')
  JsonObject get riskLevel;

  @BuiltValueField(wireName: r'success')
  bool get success;

  CommandHistory._();

  factory CommandHistory([void updates(CommandHistoryBuilder b)]) = _$CommandHistory;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CommandHistoryBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CommandHistory> get serializer => _$CommandHistorySerializer();
}

class _$CommandHistorySerializer implements PrimitiveSerializer<CommandHistory> {
  @override
  final Iterable<Type> types = const [CommandHistory, _$CommandHistory];

  @override
  final String wireName = r'CommandHistory';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CommandHistory object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    yield r'command';
    yield serializers.serialize(
      object.command,
      specifiedType: const FullType(String),
    );
    if (object.outputCommaOmitempty != null) {
      yield r'output,omitempty';
      yield serializers.serialize(
        object.outputCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.stderrCommaOmitempty != null) {
      yield r'stderr,omitempty';
      yield serializers.serialize(
        object.stderrCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    yield r'exit_code';
    yield serializers.serialize(
      object.exitCode,
      specifiedType: const FullType(int),
    );
    yield r'timestamp';
    yield serializers.serialize(
      object.timestamp,
      specifiedType: const FullType(String),
    );
    yield r'working_dir';
    yield serializers.serialize(
      object.workingDir,
      specifiedType: const FullType(String),
    );
    yield r'duration_ms';
    yield serializers.serialize(
      object.durationMs,
      specifiedType: const FullType(JsonObject),
    );
    yield r'risk_level';
    yield serializers.serialize(
      object.riskLevel,
      specifiedType: const FullType(JsonObject),
    );
    yield r'success';
    yield serializers.serialize(
      object.success,
      specifiedType: const FullType(bool),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    CommandHistory object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CommandHistoryBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.id = valueDes;
          break;
        case r'command':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.command = valueDes;
          break;
        case r'output,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.outputCommaOmitempty = valueDes;
          break;
        case r'stderr,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.stderrCommaOmitempty = valueDes;
          break;
        case r'exit_code':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.exitCode = valueDes;
          break;
        case r'timestamp':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.timestamp = valueDes;
          break;
        case r'working_dir':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.workingDir = valueDes;
          break;
        case r'duration_ms':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.durationMs = valueDes;
          break;
        case r'risk_level':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.riskLevel = valueDes;
          break;
        case r'success':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.success = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  CommandHistory deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CommandHistoryBuilder();
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

