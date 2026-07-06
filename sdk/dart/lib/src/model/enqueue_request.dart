//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'enqueue_request.g.dart';

/// EnqueueRequest
///
/// Properties:
/// * [type] 
/// * [priority] 
/// * [taskId] 
/// * [prompt] 
/// * [sessionId] 
/// * [requiredCaps] 
/// * [payload] 
@BuiltValue()
abstract class EnqueueRequest implements Built<EnqueueRequest, EnqueueRequestBuilder> {
  @BuiltValueField(wireName: r'type')
  String get type;

  @BuiltValueField(wireName: r'priority')
  int? get priority;

  @BuiltValueField(wireName: r'task_id')
  String? get taskId;

  @BuiltValueField(wireName: r'prompt')
  String get prompt;

  @BuiltValueField(wireName: r'session_id')
  String? get sessionId;

  @BuiltValueField(wireName: r'required_caps')
  String? get requiredCaps;

  @BuiltValueField(wireName: r'payload')
  String? get payload;

  EnqueueRequest._();

  factory EnqueueRequest([void updates(EnqueueRequestBuilder b)]) = _$EnqueueRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(EnqueueRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<EnqueueRequest> get serializer => _$EnqueueRequestSerializer();
}

class _$EnqueueRequestSerializer implements PrimitiveSerializer<EnqueueRequest> {
  @override
  final Iterable<Type> types = const [EnqueueRequest, _$EnqueueRequest];

  @override
  final String wireName = r'EnqueueRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    EnqueueRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'type';
    yield serializers.serialize(
      object.type,
      specifiedType: const FullType(String),
    );
    if (object.priority != null) {
      yield r'priority';
      yield serializers.serialize(
        object.priority,
        specifiedType: const FullType(int),
      );
    }
    if (object.taskId != null) {
      yield r'task_id';
      yield serializers.serialize(
        object.taskId,
        specifiedType: const FullType(String),
      );
    }
    yield r'prompt';
    yield serializers.serialize(
      object.prompt,
      specifiedType: const FullType(String),
    );
    if (object.sessionId != null) {
      yield r'session_id';
      yield serializers.serialize(
        object.sessionId,
        specifiedType: const FullType(String),
      );
    }
    if (object.requiredCaps != null) {
      yield r'required_caps';
      yield serializers.serialize(
        object.requiredCaps,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.payload != null) {
      yield r'payload';
      yield serializers.serialize(
        object.payload,
        specifiedType: const FullType.nullable(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    EnqueueRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required EnqueueRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'type':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.type = valueDes;
          break;
        case r'priority':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.priority = valueDes;
          break;
        case r'task_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.taskId = valueDes;
          break;
        case r'prompt':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.prompt = valueDes;
          break;
        case r'session_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.sessionId = valueDes;
          break;
        case r'required_caps':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.requiredCaps = valueDes;
          break;
        case r'payload':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.payload = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  EnqueueRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = EnqueueRequestBuilder();
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

