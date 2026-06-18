//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'daemon_service.g.dart';

/// DaemonService
///
/// Properties:
/// * [pidFile] 
/// * [stateDir] 
/// * [binPath] 
/// * [controller] 
@BuiltValue()
abstract class DaemonService implements Built<DaemonService, DaemonServiceBuilder> {
  @BuiltValueField(wireName: r'pidFile')
  String? get pidFile;

  @BuiltValueField(wireName: r'stateDir')
  String? get stateDir;

  @BuiltValueField(wireName: r'binPath')
  String? get binPath;

  @BuiltValueField(wireName: r'controller')
  JsonObject? get controller;

  DaemonService._();

  factory DaemonService([void updates(DaemonServiceBuilder b)]) = _$DaemonService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(DaemonServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<DaemonService> get serializer => _$DaemonServiceSerializer();
}

class _$DaemonServiceSerializer implements PrimitiveSerializer<DaemonService> {
  @override
  final Iterable<Type> types = const [DaemonService, _$DaemonService];

  @override
  final String wireName = r'DaemonService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    DaemonService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.pidFile != null) {
      yield r'pidFile';
      yield serializers.serialize(
        object.pidFile,
        specifiedType: const FullType(String),
      );
    }
    if (object.stateDir != null) {
      yield r'stateDir';
      yield serializers.serialize(
        object.stateDir,
        specifiedType: const FullType(String),
      );
    }
    if (object.binPath != null) {
      yield r'binPath';
      yield serializers.serialize(
        object.binPath,
        specifiedType: const FullType(String),
      );
    }
    if (object.controller != null) {
      yield r'controller';
      yield serializers.serialize(
        object.controller,
        specifiedType: const FullType(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    DaemonService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required DaemonServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'pidFile':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.pidFile = valueDes;
          break;
        case r'stateDir':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.stateDir = valueDes;
          break;
        case r'binPath':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.binPath = valueDes;
          break;
        case r'controller':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.controller = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  DaemonService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = DaemonServiceBuilder();
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

