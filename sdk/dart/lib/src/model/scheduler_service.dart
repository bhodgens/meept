//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'scheduler_service.g.dart';

/// SchedulerService
///
/// Properties:
/// * [scheduler] 
@BuiltValue()
abstract class SchedulerService implements Built<SchedulerService, SchedulerServiceBuilder> {
  @BuiltValueField(wireName: r'scheduler')
  JsonObject? get scheduler;

  SchedulerService._();

  factory SchedulerService([void updates(SchedulerServiceBuilder b)]) = _$SchedulerService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SchedulerServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SchedulerService> get serializer => _$SchedulerServiceSerializer();
}

class _$SchedulerServiceSerializer implements PrimitiveSerializer<SchedulerService> {
  @override
  final Iterable<Type> types = const [SchedulerService, _$SchedulerService];

  @override
  final String wireName = r'SchedulerService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SchedulerService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.scheduler != null) {
      yield r'scheduler';
      yield serializers.serialize(
        object.scheduler,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    SchedulerService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SchedulerServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'scheduler':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.scheduler = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SchedulerService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SchedulerServiceBuilder();
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

