//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'get_messages_request.g.dart';

/// GetMessagesRequest
///
/// Properties:
/// * [id] 
/// * [offset] 
/// * [limit] 
@BuiltValue()
abstract class GetMessagesRequest implements Built<GetMessagesRequest, GetMessagesRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'offset')
  int? get offset;

  @BuiltValueField(wireName: r'limit')
  int? get limit;

  GetMessagesRequest._();

  factory GetMessagesRequest([void updates(GetMessagesRequestBuilder b)]) = _$GetMessagesRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(GetMessagesRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<GetMessagesRequest> get serializer => _$GetMessagesRequestSerializer();
}

class _$GetMessagesRequestSerializer implements PrimitiveSerializer<GetMessagesRequest> {
  @override
  final Iterable<Type> types = const [GetMessagesRequest, _$GetMessagesRequest];

  @override
  final String wireName = r'GetMessagesRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    GetMessagesRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    if (object.offset != null) {
      yield r'offset';
      yield serializers.serialize(
        object.offset,
        specifiedType: const FullType(int),
      );
    }
    if (object.limit != null) {
      yield r'limit';
      yield serializers.serialize(
        object.limit,
        specifiedType: const FullType(int),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    GetMessagesRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required GetMessagesRequestBuilder result,
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
        case r'offset':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.offset = valueDes;
          break;
        case r'limit':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.limit = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  GetMessagesRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = GetMessagesRequestBuilder();
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

