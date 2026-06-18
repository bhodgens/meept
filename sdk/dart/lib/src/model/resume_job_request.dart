//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'resume_job_request.g.dart';

/// ResumeJobRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class ResumeJobRequest implements Built<ResumeJobRequest, ResumeJobRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  ResumeJobRequest._();

  factory ResumeJobRequest([void updates(ResumeJobRequestBuilder b)]) = _$ResumeJobRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ResumeJobRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ResumeJobRequest> get serializer => _$ResumeJobRequestSerializer();
}

class _$ResumeJobRequestSerializer implements PrimitiveSerializer<ResumeJobRequest> {
  @override
  final Iterable<Type> types = const [ResumeJobRequest, _$ResumeJobRequest];

  @override
  final String wireName = r'ResumeJobRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ResumeJobRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    ResumeJobRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ResumeJobRequestBuilder result,
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
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ResumeJobRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ResumeJobRequestBuilder();
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

