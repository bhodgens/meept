//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'create_task_request.g.dart';

/// CreateTaskRequest
///
/// Properties:
/// * [name] 
/// * [descriptionCommaOmitempty] 
/// * [sessionIdCommaOmitempty] 
@BuiltValue()
abstract class CreateTaskRequest implements Built<CreateTaskRequest, CreateTaskRequestBuilder> {
  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'description,omitempty')
  String? get descriptionCommaOmitempty;

  @BuiltValueField(wireName: r'session_id,omitempty')
  String? get sessionIdCommaOmitempty;

  CreateTaskRequest._();

  factory CreateTaskRequest([void updates(CreateTaskRequestBuilder b)]) = _$CreateTaskRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CreateTaskRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CreateTaskRequest> get serializer => _$CreateTaskRequestSerializer();
}

class _$CreateTaskRequestSerializer implements PrimitiveSerializer<CreateTaskRequest> {
  @override
  final Iterable<Type> types = const [CreateTaskRequest, _$CreateTaskRequest];

  @override
  final String wireName = r'CreateTaskRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CreateTaskRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'name';
    yield serializers.serialize(
      object.name,
      specifiedType: const FullType(String),
    );
    if (object.descriptionCommaOmitempty != null) {
      yield r'description,omitempty';
      yield serializers.serialize(
        object.descriptionCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.sessionIdCommaOmitempty != null) {
      yield r'session_id,omitempty';
      yield serializers.serialize(
        object.sessionIdCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    CreateTaskRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CreateTaskRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'name':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.name = valueDes;
          break;
        case r'description,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.descriptionCommaOmitempty = valueDes;
          break;
        case r'session_id,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.sessionIdCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  CreateTaskRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CreateTaskRequestBuilder();
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

