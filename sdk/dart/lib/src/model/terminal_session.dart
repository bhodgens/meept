//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'terminal_session.g.dart';

/// TerminalSession
///
/// Properties:
/// * [ID] 
/// * [workingDir] 
/// * [createdAt] 
/// * [lastUsed] 
/// * [commandCount] 
@BuiltValue()
abstract class TerminalSession implements Built<TerminalSession, TerminalSessionBuilder> {
  @BuiltValueField(wireName: r'ID')
  String? get ID;

  @BuiltValueField(wireName: r'WorkingDir')
  String? get workingDir;

  @BuiltValueField(wireName: r'CreatedAt')
  String? get createdAt;

  @BuiltValueField(wireName: r'LastUsed')
  String? get lastUsed;

  @BuiltValueField(wireName: r'CommandCount')
  int? get commandCount;

  TerminalSession._();

  factory TerminalSession([void updates(TerminalSessionBuilder b)]) = _$TerminalSession;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(TerminalSessionBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<TerminalSession> get serializer => _$TerminalSessionSerializer();
}

class _$TerminalSessionSerializer implements PrimitiveSerializer<TerminalSession> {
  @override
  final Iterable<Type> types = const [TerminalSession, _$TerminalSession];

  @override
  final String wireName = r'TerminalSession';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    TerminalSession object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.ID != null) {
      yield r'ID';
      yield serializers.serialize(
        object.ID,
        specifiedType: const FullType(String),
      );
    }
    if (object.workingDir != null) {
      yield r'WorkingDir';
      yield serializers.serialize(
        object.workingDir,
        specifiedType: const FullType(String),
      );
    }
    if (object.createdAt != null) {
      yield r'CreatedAt';
      yield serializers.serialize(
        object.createdAt,
        specifiedType: const FullType(String),
      );
    }
    if (object.lastUsed != null) {
      yield r'LastUsed';
      yield serializers.serialize(
        object.lastUsed,
        specifiedType: const FullType(String),
      );
    }
    if (object.commandCount != null) {
      yield r'CommandCount';
      yield serializers.serialize(
        object.commandCount,
        specifiedType: const FullType(int),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    TerminalSession object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required TerminalSessionBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'ID':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.ID = valueDes;
          break;
        case r'WorkingDir':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.workingDir = valueDes;
          break;
        case r'CreatedAt':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.createdAt = valueDes;
          break;
        case r'LastUsed':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.lastUsed = valueDes;
          break;
        case r'CommandCount':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.commandCount = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  TerminalSession deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = TerminalSessionBuilder();
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

