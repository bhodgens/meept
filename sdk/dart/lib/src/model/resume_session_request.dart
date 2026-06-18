//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'resume_session_request.g.dart';

/// ResumeSessionRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class ResumeSessionRequest implements Built<ResumeSessionRequest, ResumeSessionRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  ResumeSessionRequest._();

  factory ResumeSessionRequest([void updates(ResumeSessionRequestBuilder b)]) = _$ResumeSessionRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ResumeSessionRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ResumeSessionRequest> get serializer => _$ResumeSessionRequestSerializer();
}

class _$ResumeSessionRequestSerializer implements PrimitiveSerializer<ResumeSessionRequest> {
  @override
  final Iterable<Type> types = const [ResumeSessionRequest, _$ResumeSessionRequest];

  @override
  final String wireName = r'ResumeSessionRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ResumeSessionRequest object, {
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
    ResumeSessionRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ResumeSessionRequestBuilder result,
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
  ResumeSessionRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ResumeSessionRequestBuilder();
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

