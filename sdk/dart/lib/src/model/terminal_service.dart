//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_collection/built_collection.dart';
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'terminal_service.g.dart';

/// TerminalService
///
/// Properties:
/// * [shellTool] 
/// * [bus] 
/// * [logger] 
/// * [history] 
/// * [historyMu] 
/// * [maxHistory] 
/// * [workingDir] 
/// * [sessionStore] 
/// * [sessionMu] 
@BuiltValue()
abstract class TerminalService implements Built<TerminalService, TerminalServiceBuilder> {
  @BuiltValueField(wireName: r'shellTool')
  JsonObject? get shellTool;

  @BuiltValueField(wireName: r'bus')
  JsonObject? get bus;

  @BuiltValueField(wireName: r'logger')
  JsonObject? get logger;

  @BuiltValueField(wireName: r'history')
  BuiltList<String>? get history;

  @BuiltValueField(wireName: r'historyMu')
  JsonObject? get historyMu;

  @BuiltValueField(wireName: r'maxHistory')
  int? get maxHistory;

  @BuiltValueField(wireName: r'workingDir')
  String? get workingDir;

  @BuiltValueField(wireName: r'sessionStore')
  String? get sessionStore;

  @BuiltValueField(wireName: r'sessionMu')
  JsonObject? get sessionMu;

  TerminalService._();

  factory TerminalService([void updates(TerminalServiceBuilder b)]) = _$TerminalService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(TerminalServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<TerminalService> get serializer => _$TerminalServiceSerializer();
}

class _$TerminalServiceSerializer implements PrimitiveSerializer<TerminalService> {
  @override
  final Iterable<Type> types = const [TerminalService, _$TerminalService];

  @override
  final String wireName = r'TerminalService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    TerminalService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.shellTool != null) {
      yield r'shellTool';
      yield serializers.serialize(
        object.shellTool,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.bus != null) {
      yield r'bus';
      yield serializers.serialize(
        object.bus,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.logger != null) {
      yield r'logger';
      yield serializers.serialize(
        object.logger,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
    if (object.history != null) {
      yield r'history';
      yield serializers.serialize(
        object.history,
        specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
      );
    }
    if (object.historyMu != null) {
      yield r'historyMu';
      yield serializers.serialize(
        object.historyMu,
        specifiedType: const FullType(JsonObject),
      );
    }
    if (object.maxHistory != null) {
      yield r'maxHistory';
      yield serializers.serialize(
        object.maxHistory,
        specifiedType: const FullType(int),
      );
    }
    if (object.workingDir != null) {
      yield r'workingDir';
      yield serializers.serialize(
        object.workingDir,
        specifiedType: const FullType(String),
      );
    }
    if (object.sessionStore != null) {
      yield r'sessionStore';
      yield serializers.serialize(
        object.sessionStore,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.sessionMu != null) {
      yield r'sessionMu';
      yield serializers.serialize(
        object.sessionMu,
        specifiedType: const FullType(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    TerminalService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required TerminalServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'shellTool':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.shellTool = valueDes;
          break;
        case r'bus':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.bus = valueDes;
          break;
        case r'logger':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.logger = valueDes;
          break;
        case r'history':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
          ) as BuiltList<String>?;
          if (valueDes == null) continue;
          result.history.replace(valueDes);
          break;
        case r'historyMu':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.historyMu = valueDes;
          break;
        case r'maxHistory':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.maxHistory = valueDes;
          break;
        case r'workingDir':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.workingDir = valueDes;
          break;
        case r'sessionStore':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.sessionStore = valueDes;
          break;
        case r'sessionMu':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.sessionMu = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  TerminalService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = TerminalServiceBuilder();
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

