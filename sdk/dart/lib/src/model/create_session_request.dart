//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'create_session_request.g.dart';

/// CreateSessionRequest
///
/// Properties:
/// * [nameCommaOmitempty] 
@BuiltValue()
abstract class CreateSessionRequest implements Built<CreateSessionRequest, CreateSessionRequestBuilder> {
  @BuiltValueField(wireName: r'name,omitempty')
  String? get nameCommaOmitempty;

  CreateSessionRequest._();

  factory CreateSessionRequest([void updates(CreateSessionRequestBuilder b)]) = _$CreateSessionRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CreateSessionRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CreateSessionRequest> get serializer => _$CreateSessionRequestSerializer();
}

class _$CreateSessionRequestSerializer implements PrimitiveSerializer<CreateSessionRequest> {
  @override
  final Iterable<Type> types = const [CreateSessionRequest, _$CreateSessionRequest];

  @override
  final String wireName = r'CreateSessionRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CreateSessionRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
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
    CreateSessionRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CreateSessionRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
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
  CreateSessionRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CreateSessionRequestBuilder();
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

