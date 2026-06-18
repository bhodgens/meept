//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'task_service.g.dart';

/// TaskService
///
/// Properties:
/// * [registry] 
@BuiltValue()
abstract class TaskService implements Built<TaskService, TaskServiceBuilder> {
  @BuiltValueField(wireName: r'registry')
  JsonObject? get registry;

  TaskService._();

  factory TaskService([void updates(TaskServiceBuilder b)]) = _$TaskService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(TaskServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<TaskService> get serializer => _$TaskServiceSerializer();
}

class _$TaskServiceSerializer implements PrimitiveSerializer<TaskService> {
  @override
  final Iterable<Type> types = const [TaskService, _$TaskService];

  @override
  final String wireName = r'TaskService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    TaskService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.registry != null) {
      yield r'registry';
      yield serializers.serialize(
        object.registry,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    TaskService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required TaskServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'registry':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.registry = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  TaskService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = TaskServiceBuilder();
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

