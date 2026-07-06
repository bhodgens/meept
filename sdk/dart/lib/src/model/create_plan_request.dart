//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'create_plan_request.g.dart';

/// CreatePlanRequest
///
/// Properties:
/// * [title] 
/// * [description] 
/// * [projectId] 
/// * [projectPath] 
/// * [sessionId] 
@BuiltValue()
abstract class CreatePlanRequest implements Built<CreatePlanRequest, CreatePlanRequestBuilder> {
  @BuiltValueField(wireName: r'title')
  String get title;

  @BuiltValueField(wireName: r'description')
  String? get description;

  @BuiltValueField(wireName: r'project_id')
  String? get projectId;

  @BuiltValueField(wireName: r'project_path')
  String? get projectPath;

  @BuiltValueField(wireName: r'session_id')
  String get sessionId;

  CreatePlanRequest._();

  factory CreatePlanRequest([void updates(CreatePlanRequestBuilder b)]) = _$CreatePlanRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CreatePlanRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CreatePlanRequest> get serializer => _$CreatePlanRequestSerializer();
}

class _$CreatePlanRequestSerializer implements PrimitiveSerializer<CreatePlanRequest> {
  @override
  final Iterable<Type> types = const [CreatePlanRequest, _$CreatePlanRequest];

  @override
  final String wireName = r'CreatePlanRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CreatePlanRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'title';
    yield serializers.serialize(
      object.title,
      specifiedType: const FullType(String),
    );
    if (object.description != null) {
      yield r'description';
      yield serializers.serialize(
        object.description,
        specifiedType: const FullType(String),
      );
    }
    if (object.projectId != null) {
      yield r'project_id';
      yield serializers.serialize(
        object.projectId,
        specifiedType: const FullType(String),
      );
    }
    if (object.projectPath != null) {
      yield r'project_path';
      yield serializers.serialize(
        object.projectPath,
        specifiedType: const FullType(String),
      );
    }
    yield r'session_id';
    yield serializers.serialize(
      object.sessionId,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    CreatePlanRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CreatePlanRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'title':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.title = valueDes;
          break;
        case r'description':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.description = valueDes;
          break;
        case r'project_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.projectId = valueDes;
          break;
        case r'project_path':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.projectPath = valueDes;
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
  CreatePlanRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CreatePlanRequestBuilder();
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

