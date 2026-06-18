//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'enable_job_request.g.dart';

/// EnableJobRequest
///
/// Properties:
/// * [id] 
/// * [enabled] 
@BuiltValue()
abstract class EnableJobRequest implements Built<EnableJobRequest, EnableJobRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'enabled')
  bool get enabled;

  EnableJobRequest._();

  factory EnableJobRequest([void updates(EnableJobRequestBuilder b)]) = _$EnableJobRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(EnableJobRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<EnableJobRequest> get serializer => _$EnableJobRequestSerializer();
}

class _$EnableJobRequestSerializer implements PrimitiveSerializer<EnableJobRequest> {
  @override
  final Iterable<Type> types = const [EnableJobRequest, _$EnableJobRequest];

  @override
  final String wireName = r'EnableJobRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    EnableJobRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    yield r'enabled';
    yield serializers.serialize(
      object.enabled,
      specifiedType: const FullType(bool),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    EnableJobRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required EnableJobRequestBuilder result,
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
        case r'enabled':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.enabled = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  EnableJobRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = EnableJobRequestBuilder();
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

