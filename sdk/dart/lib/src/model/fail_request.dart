//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'fail_request.g.dart';

/// FailRequest
///
/// Properties:
/// * [jobId] 
/// * [error] 
@BuiltValue()
abstract class FailRequest implements Built<FailRequest, FailRequestBuilder> {
  @BuiltValueField(wireName: r'job_id')
  String get jobId;

  @BuiltValueField(wireName: r'error')
  String get error;

  FailRequest._();

  factory FailRequest([void updates(FailRequestBuilder b)]) = _$FailRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(FailRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<FailRequest> get serializer => _$FailRequestSerializer();
}

class _$FailRequestSerializer implements PrimitiveSerializer<FailRequest> {
  @override
  final Iterable<Type> types = const [FailRequest, _$FailRequest];

  @override
  final String wireName = r'FailRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    FailRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'job_id';
    yield serializers.serialize(
      object.jobId,
      specifiedType: const FullType(String),
    );
    yield r'error';
    yield serializers.serialize(
      object.error,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    FailRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required FailRequestBuilder result,
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
        case r'error':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.error = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  FailRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = FailRequestBuilder();
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

