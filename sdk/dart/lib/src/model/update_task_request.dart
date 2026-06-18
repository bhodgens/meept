//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'update_task_request.g.dart';

/// UpdateTaskRequest
///
/// Properties:
/// * [id] 
/// * [stateCommaOmitempty] 
/// * [nameCommaOmitempty] 
@BuiltValue()
abstract class UpdateTaskRequest implements Built<UpdateTaskRequest, UpdateTaskRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'state,omitempty')
  String? get stateCommaOmitempty;

  @BuiltValueField(wireName: r'name,omitempty')
  String? get nameCommaOmitempty;

  UpdateTaskRequest._();

  factory UpdateTaskRequest([void updates(UpdateTaskRequestBuilder b)]) = _$UpdateTaskRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(UpdateTaskRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<UpdateTaskRequest> get serializer => _$UpdateTaskRequestSerializer();
}

class _$UpdateTaskRequestSerializer implements PrimitiveSerializer<UpdateTaskRequest> {
  @override
  final Iterable<Type> types = const [UpdateTaskRequest, _$UpdateTaskRequest];

  @override
  final String wireName = r'UpdateTaskRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    UpdateTaskRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    if (object.stateCommaOmitempty != null) {
      yield r'state,omitempty';
      yield serializers.serialize(
        object.stateCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.nameCommaOmitempty != null) {
      yield r'name,omitempty';
      yield serializers.serialize(
        object.nameCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    UpdateTaskRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required UpdateTaskRequestBuilder result,
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
        case r'state,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.stateCommaOmitempty = valueDes;
          break;
        case r'name,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.nameCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  UpdateTaskRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = UpdateTaskRequestBuilder();
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

