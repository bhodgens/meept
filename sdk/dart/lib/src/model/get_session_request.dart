//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'get_session_request.g.dart';

/// GetSessionRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class GetSessionRequest implements Built<GetSessionRequest, GetSessionRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  GetSessionRequest._();

  factory GetSessionRequest([void updates(GetSessionRequestBuilder b)]) = _$GetSessionRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(GetSessionRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<GetSessionRequest> get serializer => _$GetSessionRequestSerializer();
}

class _$GetSessionRequestSerializer implements PrimitiveSerializer<GetSessionRequest> {
  @override
  final Iterable<Type> types = const [GetSessionRequest, _$GetSessionRequest];

  @override
  final String wireName = r'GetSessionRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    GetSessionRequest object, {
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
    GetSessionRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required GetSessionRequestBuilder result,
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
  GetSessionRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = GetSessionRequestBuilder();
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

