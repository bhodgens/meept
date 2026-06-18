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
/// * [descriptionCommaOmitempty] 
/// * [projectIdCommaOmitempty] 
/// * [projectPathCommaOmitempty] 
/// * [sessionId] 
@BuiltValue()
abstract class CreatePlanRequest implements Built<CreatePlanRequest, CreatePlanRequestBuilder> {
  @BuiltValueField(wireName: r'title')
  String get title;

  @BuiltValueField(wireName: r'description,omitempty')
  String? get descriptionCommaOmitempty;

  @BuiltValueField(wireName: r'project_id,omitempty')
  String? get projectIdCommaOmitempty;

  @BuiltValueField(wireName: r'project_path,omitempty')
  String? get projectPathCommaOmitempty;

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
    if (object.descriptionCommaOmitempty != null) {
      yield r'description,omitempty';
      yield serializers.serialize(
        object.descriptionCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.projectIdCommaOmitempty != null) {
      yield r'project_id,omitempty';
      yield serializers.serialize(
        object.projectIdCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.projectPathCommaOmitempty != null) {
      yield r'project_path,omitempty';
      yield serializers.serialize(
        object.projectPathCommaOmitempty,
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
        case r'description,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.descriptionCommaOmitempty = valueDes;
          break;
        case r'project_id,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.projectIdCommaOmitempty = valueDes;
          break;
        case r'project_path,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.projectPathCommaOmitempty = valueDes;
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

