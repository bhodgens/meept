//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'status_response.g.dart';

/// StatusResponse
///
/// Properties:
/// * [enabled] 
/// * [lastCycleCommaOmitempty] 
/// * [skillsLearned] 
/// * [pendingTasks] 
@BuiltValue()
abstract class StatusResponse implements Built<StatusResponse, StatusResponseBuilder> {
  @BuiltValueField(wireName: r'enabled')
  bool get enabled;

  @BuiltValueField(wireName: r'last_cycle,omitempty')
  String? get lastCycleCommaOmitempty;

  @BuiltValueField(wireName: r'skills_learned')
  int get skillsLearned;

  @BuiltValueField(wireName: r'pending_tasks')
  int get pendingTasks;

  StatusResponse._();

  factory StatusResponse([void updates(StatusResponseBuilder b)]) = _$StatusResponse;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(StatusResponseBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<StatusResponse> get serializer => _$StatusResponseSerializer();
}

class _$StatusResponseSerializer implements PrimitiveSerializer<StatusResponse> {
  @override
  final Iterable<Type> types = const [StatusResponse, _$StatusResponse];

  @override
  final String wireName = r'StatusResponse';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    StatusResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'enabled';
    yield serializers.serialize(
      object.enabled,
      specifiedType: const FullType(bool),
    );
    if (object.lastCycleCommaOmitempty != null) {
      yield r'last_cycle,omitempty';
      yield serializers.serialize(
        object.lastCycleCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    yield r'skills_learned';
    yield serializers.serialize(
      object.skillsLearned,
      specifiedType: const FullType(int),
    );
    yield r'pending_tasks';
    yield serializers.serialize(
      object.pendingTasks,
      specifiedType: const FullType(int),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    StatusResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required StatusResponseBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'enabled':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.enabled = valueDes;
          break;
        case r'last_cycle,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.lastCycleCommaOmitempty = valueDes;
          break;
        case r'skills_learned':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.skillsLearned = valueDes;
          break;
        case r'pending_tasks':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.pendingTasks = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  StatusResponse deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = StatusResponseBuilder();
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

