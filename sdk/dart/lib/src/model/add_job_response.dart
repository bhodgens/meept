//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'add_job_response.g.dart';

/// AddJobResponse
///
/// Properties:
/// * [id] 
/// * [name] 
/// * [schedule] 
/// * [enabled] 
/// * [lastRunCommaOmitempty] 
/// * [nextRunCommaOmitempty] 
/// * [lastErrorCommaOmitempty] 
/// * [runCount] 
/// * [isRunning] 
@BuiltValue()
abstract class AddJobResponse implements Built<AddJobResponse, AddJobResponseBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'schedule')
  String get schedule;

  @BuiltValueField(wireName: r'enabled')
  bool get enabled;

  @BuiltValueField(wireName: r'last_run,omitempty')
  String? get lastRunCommaOmitempty;

  @BuiltValueField(wireName: r'next_run,omitempty')
  String? get nextRunCommaOmitempty;

  @BuiltValueField(wireName: r'last_error,omitempty')
  String? get lastErrorCommaOmitempty;

  @BuiltValueField(wireName: r'run_count')
  int get runCount;

  @BuiltValueField(wireName: r'is_running')
  bool get isRunning;

  AddJobResponse._();

  factory AddJobResponse([void updates(AddJobResponseBuilder b)]) = _$AddJobResponse;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(AddJobResponseBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<AddJobResponse> get serializer => _$AddJobResponseSerializer();
}

class _$AddJobResponseSerializer implements PrimitiveSerializer<AddJobResponse> {
  @override
  final Iterable<Type> types = const [AddJobResponse, _$AddJobResponse];

  @override
  final String wireName = r'AddJobResponse';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    AddJobResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    yield r'name';
    yield serializers.serialize(
      object.name,
      specifiedType: const FullType(String),
    );
    yield r'schedule';
    yield serializers.serialize(
      object.schedule,
      specifiedType: const FullType(String),
    );
    yield r'enabled';
    yield serializers.serialize(
      object.enabled,
      specifiedType: const FullType(bool),
    );
    if (object.lastRunCommaOmitempty != null) {
      yield r'last_run,omitempty';
      yield serializers.serialize(
        object.lastRunCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.nextRunCommaOmitempty != null) {
      yield r'next_run,omitempty';
      yield serializers.serialize(
        object.nextRunCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.lastErrorCommaOmitempty != null) {
      yield r'last_error,omitempty';
      yield serializers.serialize(
        object.lastErrorCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    yield r'run_count';
    yield serializers.serialize(
      object.runCount,
      specifiedType: const FullType(int),
    );
    yield r'is_running';
    yield serializers.serialize(
      object.isRunning,
      specifiedType: const FullType(bool),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    AddJobResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required AddJobResponseBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.id = valueDes;
          break;
        case r'name':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.name = valueDes;
          break;
        case r'schedule':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.schedule = valueDes;
          break;
        case r'enabled':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.enabled = valueDes;
          break;
        case r'last_run,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.lastRunCommaOmitempty = valueDes;
          break;
        case r'next_run,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.nextRunCommaOmitempty = valueDes;
          break;
        case r'last_error,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.lastErrorCommaOmitempty = valueDes;
          break;
        case r'run_count':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.runCount = valueDes;
          break;
        case r'is_running':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.isRunning = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  AddJobResponse deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = AddJobResponseBuilder();
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

