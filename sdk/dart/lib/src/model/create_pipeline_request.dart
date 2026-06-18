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
/// * [idCommaOmitempty] 
/// * [name] 
/// * [descriptionCommaOmitempty] 
/// * [stepsCommaOmitempty] 
/// * [metadataCommaOmitempty] 
@BuiltValue()
abstract class CreatePipelineRequest implements Built<CreatePipelineRequest, CreatePipelineRequestBuilder> {
  @BuiltValueField(wireName: r'id,omitempty')
  String? get idCommaOmitempty;

  @BuiltValueField(wireName: r'name')
  String get name;

  @BuiltValueField(wireName: r'description,omitempty')
  String? get descriptionCommaOmitempty;

  @BuiltValueField(wireName: r'steps,omitempty')
  BuiltList<String>? get stepsCommaOmitempty;

  @BuiltValueField(wireName: r'metadata,omitempty')
  String? get metadataCommaOmitempty;

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
    if (object.idCommaOmitempty != null) {
      yield r'id,omitempty';
      yield serializers.serialize(
        object.idCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
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
    if (object.stepsCommaOmitempty != null) {
      yield r'steps,omitempty';
      yield serializers.serialize(
        object.stepsCommaOmitempty,
        specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
      );
    }
    if (object.metadataCommaOmitempty != null) {
      yield r'metadata,omitempty';
      yield serializers.serialize(
        object.metadataCommaOmitempty,
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
        case r'id,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.idCommaOmitempty = valueDes;
          break;
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
        case r'steps,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
          ) as BuiltList<String>?;
          if (valueDes == null) continue;
          result.stepsCommaOmitempty.replace(valueDes);
          break;
        case r'metadata,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.metadataCommaOmitempty = valueDes;
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

