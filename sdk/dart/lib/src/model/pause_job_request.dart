//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'pause_job_request.g.dart';

/// PauseJobRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class PauseJobRequest implements Built<PauseJobRequest, PauseJobRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  PauseJobRequest._();

  factory PauseJobRequest([void updates(PauseJobRequestBuilder b)]) = _$PauseJobRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(PauseJobRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<PauseJobRequest> get serializer => _$PauseJobRequestSerializer();
}

class _$PauseJobRequestSerializer implements PrimitiveSerializer<PauseJobRequest> {
  @override
  final Iterable<Type> types = const [PauseJobRequest, _$PauseJobRequest];

  @override
  final String wireName = r'PauseJobRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    PauseJobRequest object, {
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
    PauseJobRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required PauseJobRequestBuilder result,
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
  PauseJobRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = PauseJobRequestBuilder();
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

