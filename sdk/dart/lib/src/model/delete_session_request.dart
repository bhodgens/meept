//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'delete_session_request.g.dart';

/// DeleteSessionRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class DeleteSessionRequest implements Built<DeleteSessionRequest, DeleteSessionRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  DeleteSessionRequest._();

  factory DeleteSessionRequest([void updates(DeleteSessionRequestBuilder b)]) = _$DeleteSessionRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(DeleteSessionRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<DeleteSessionRequest> get serializer => _$DeleteSessionRequestSerializer();
}

class _$DeleteSessionRequestSerializer implements PrimitiveSerializer<DeleteSessionRequest> {
  @override
  final Iterable<Type> types = const [DeleteSessionRequest, _$DeleteSessionRequest];

  @override
  final String wireName = r'DeleteSessionRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    DeleteSessionRequest object, {
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
    DeleteSessionRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required DeleteSessionRequestBuilder result,
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
  DeleteSessionRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = DeleteSessionRequestBuilder();
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

