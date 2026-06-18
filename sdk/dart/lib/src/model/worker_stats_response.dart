//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_collection/built_collection.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'worker_stats_response.g.dart';

/// WorkerStatsResponse
///
/// Properties:
/// * [totalWorkers] 
/// * [idleWorkers] 
/// * [busyWorkers] 
/// * [errorWorkers] 
/// * [workerStats] 
@BuiltValue()
abstract class WorkerStatsResponse implements Built<WorkerStatsResponse, WorkerStatsResponseBuilder> {
  @BuiltValueField(wireName: r'total_workers')
  int get totalWorkers;

  @BuiltValueField(wireName: r'idle_workers')
  int get idleWorkers;

  @BuiltValueField(wireName: r'busy_workers')
  int get busyWorkers;

  @BuiltValueField(wireName: r'error_workers')
  int get errorWorkers;

  @BuiltValueField(wireName: r'worker_stats')
  BuiltList<String>? get workerStats;

  WorkerStatsResponse._();

  factory WorkerStatsResponse([void updates(WorkerStatsResponseBuilder b)]) = _$WorkerStatsResponse;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(WorkerStatsResponseBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<WorkerStatsResponse> get serializer => _$WorkerStatsResponseSerializer();
}

class _$WorkerStatsResponseSerializer implements PrimitiveSerializer<WorkerStatsResponse> {
  @override
  final Iterable<Type> types = const [WorkerStatsResponse, _$WorkerStatsResponse];

  @override
  final String wireName = r'WorkerStatsResponse';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    WorkerStatsResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'total_workers';
    yield serializers.serialize(
      object.totalWorkers,
      specifiedType: const FullType(int),
    );
    yield r'idle_workers';
    yield serializers.serialize(
      object.idleWorkers,
      specifiedType: const FullType(int),
    );
    yield r'busy_workers';
    yield serializers.serialize(
      object.busyWorkers,
      specifiedType: const FullType(int),
    );
    yield r'error_workers';
    yield serializers.serialize(
      object.errorWorkers,
      specifiedType: const FullType(int),
    );
    yield r'worker_stats';
    yield object.workerStats == null ? null : serializers.serialize(
      object.workerStats,
      specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    WorkerStatsResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required WorkerStatsResponseBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'total_workers':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.totalWorkers = valueDes;
          break;
        case r'idle_workers':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.idleWorkers = valueDes;
          break;
        case r'busy_workers':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.busyWorkers = valueDes;
          break;
        case r'error_workers':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.errorWorkers = valueDes;
          break;
        case r'worker_stats':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
          ) as BuiltList<String>?;
          if (valueDes == null) continue;
          result.workerStats.replace(valueDes);
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  WorkerStatsResponse deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = WorkerStatsResponseBuilder();
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

