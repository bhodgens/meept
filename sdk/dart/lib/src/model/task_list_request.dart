//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'task_list_request.g.dart';

/// TaskListRequest
///
/// Properties:
/// * [limit] 
/// * [sessionId] 
@BuiltValue()
abstract class TaskListRequest implements Built<TaskListRequest, TaskListRequestBuilder> {
  @BuiltValueField(wireName: r'limit')
  int? get limit;

  @BuiltValueField(wireName: r'session_id')
  String? get sessionId;

  TaskListRequest._();

  factory TaskListRequest([void updates(TaskListRequestBuilder b)]) = _$TaskListRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(TaskListRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<TaskListRequest> get serializer => _$TaskListRequestSerializer();
}

class _$TaskListRequestSerializer implements PrimitiveSerializer<TaskListRequest> {
  @override
  final Iterable<Type> types = const [TaskListRequest, _$TaskListRequest];

  @override
  final String wireName = r'TaskListRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    TaskListRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.limit != null) {
      yield r'limit';
      yield serializers.serialize(
        object.limit,
        specifiedType: const FullType(int),
      );
    }
    if (object.sessionId != null) {
      yield r'session_id';
      yield serializers.serialize(
        object.sessionId,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    TaskListRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required TaskListRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'limit':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.limit = valueDes;
          break;
        case r'session_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.sessionId = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  TaskListRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = TaskListRequestBuilder();
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

