//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'complete_request.g.dart';

/// CompleteRequest
///
/// Properties:
/// * [jobId] 
/// * [resultCommaOmitempty] 
@BuiltValue()
abstract class CompleteRequest implements Built<CompleteRequest, CompleteRequestBuilder> {
  @BuiltValueField(wireName: r'job_id')
  String get jobId;

  @BuiltValueField(wireName: r'result,omitempty')
  JsonObject? get resultCommaOmitempty;

  CompleteRequest._();

  factory CompleteRequest([void updates(CompleteRequestBuilder b)]) = _$CompleteRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CompleteRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CompleteRequest> get serializer => _$CompleteRequestSerializer();
}

class _$CompleteRequestSerializer implements PrimitiveSerializer<CompleteRequest> {
  @override
  final Iterable<Type> types = const [CompleteRequest, _$CompleteRequest];

  @override
  final String wireName = r'CompleteRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CompleteRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'job_id';
    yield serializers.serialize(
      object.jobId,
      specifiedType: const FullType(String),
    );
    if (object.resultCommaOmitempty != null) {
      yield r'result,omitempty';
      yield serializers.serialize(
        object.resultCommaOmitempty,
        specifiedType: const FullType(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    CompleteRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CompleteRequestBuilder result,
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
        case r'result,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.resultCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  CompleteRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CompleteRequestBuilder();
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

