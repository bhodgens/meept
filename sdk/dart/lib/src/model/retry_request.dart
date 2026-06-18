//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'retry_request.g.dart';

/// RetryRequest
///
/// Properties:
/// * [jobId] 
@BuiltValue()
abstract class RetryRequest implements Built<RetryRequest, RetryRequestBuilder> {
  @BuiltValueField(wireName: r'job_id')
  String get jobId;

  RetryRequest._();

  factory RetryRequest([void updates(RetryRequestBuilder b)]) = _$RetryRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(RetryRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<RetryRequest> get serializer => _$RetryRequestSerializer();
}

class _$RetryRequestSerializer implements PrimitiveSerializer<RetryRequest> {
  @override
  final Iterable<Type> types = const [RetryRequest, _$RetryRequest];

  @override
  final String wireName = r'RetryRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    RetryRequest object, {
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
    RetryRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required RetryRequestBuilder result,
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
  RetryRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = RetryRequestBuilder();
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

