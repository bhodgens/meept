//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_collection/built_collection.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'create_pipeline_request.g.dart';

/// CreatePipelineRequest
///
/// Properties:
/// * [id] 
/// * [name] 
/// * [description] 
/// * [steps] 
/// * [metadata] 
@BuiltValue()
abstract class CreatePipelineRequest implements Built<CreatePipelineRequest, CreatePipelineRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String? get id;

  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'description')
  String? get description;

  @BuiltValueField(wireName: r'steps')
  BuiltList<String>? get steps;

  @BuiltValueField(wireName: r'metadata')
  String? get metadata;

  CreatePipelineRequest._();

  factory CreatePipelineRequest([void updates(CreatePipelineRequestBuilder b)]) = _$CreatePipelineRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CreatePipelineRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CreatePipelineRequest> get serializer => _$CreatePipelineRequestSerializer();
}

class _$CreatePipelineRequestSerializer implements PrimitiveSerializer<CreatePipelineRequest> {
  @override
  final Iterable<Type> types = const [CreatePipelineRequest, _$CreatePipelineRequest];

  @override
  final String wireName = r'CreatePipelineRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CreatePipelineRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.id != null) {
      yield r'id';
      yield serializers.serialize(
        object.id,
        specifiedType: const FullType(String),
      );
    }
    yield r'name';
    yield serializers.serialize(
      object.name,
      specifiedType: const FullType(String),
    );
    if (object.description != null) {
      yield r'description';
      yield serializers.serialize(
        object.description,
        specifiedType: const FullType(String),
      );
    }
    if (object.steps != null) {
      yield r'steps';
      yield serializers.serialize(
        object.steps,
        specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
      );
    }
    if (object.metadata != null) {
      yield r'metadata';
      yield serializers.serialize(
        object.metadata,
        specifiedType: const FullType.nullable(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    CreatePipelineRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CreatePipelineRequestBuilder result,
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
        case r'description':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.description = valueDes;
          break;
        case r'steps':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
          ) as BuiltList<String>?;
          if (valueDes == null) continue;
          result.steps.replace(valueDes);
          break;
        case r'metadata':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.metadata = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  CreatePipelineRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CreatePipelineRequestBuilder();
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

