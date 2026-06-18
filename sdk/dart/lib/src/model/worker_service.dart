//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'worker_service.g.dart';

/// WorkerService
///
/// Properties:
/// * [pool] 
@BuiltValue()
abstract class WorkerService implements Built<WorkerService, WorkerServiceBuilder> {
  @BuiltValueField(wireName: r'pool')
  JsonObject? get pool;

  WorkerService._();

  factory WorkerService([void updates(WorkerServiceBuilder b)]) = _$WorkerService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(WorkerServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<WorkerService> get serializer => _$WorkerServiceSerializer();
}

class _$WorkerServiceSerializer implements PrimitiveSerializer<WorkerService> {
  @override
  final Iterable<Type> types = const [WorkerService, _$WorkerService];

  @override
  final String wireName = r'WorkerService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    WorkerService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.pool != null) {
      yield r'pool';
      yield serializers.serialize(
        object.pool,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    WorkerService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required WorkerServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'pool':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.pool = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  WorkerService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = WorkerServiceBuilder();
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

