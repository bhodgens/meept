//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'get_request.g.dart';

/// GetRequest
///
/// Properties:
/// * [jobId] 
@BuiltValue()
abstract class GetRequest implements Built<GetRequest, GetRequestBuilder> {
  @BuiltValueField(wireName: r'job_id')
  String get jobId;

  GetRequest._();

  factory GetRequest([void updates(GetRequestBuilder b)]) = _$GetRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(GetRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<GetRequest> get serializer => _$GetRequestSerializer();
}

class _$GetRequestSerializer implements PrimitiveSerializer<GetRequest> {
  @override
  final Iterable<Type> types = const [GetRequest, _$GetRequest];

  @override
  final String wireName = r'GetRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    GetRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'job_id';
    yield serializers.serialize(
      object.jobId,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    GetRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required GetRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'job_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.jobId = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  GetRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = GetRequestBuilder();
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

