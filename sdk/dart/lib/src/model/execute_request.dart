//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'execute_request.g.dart';

/// ExecuteRequest
///
/// Properties:
/// * [slug] 
/// * [prompt] 
@BuiltValue()
abstract class ExecuteRequest implements Built<ExecuteRequest, ExecuteRequestBuilder> {
  @BuiltValueField(wireName: r'slug')
  String get slug;

  @BuiltValueField(wireName: r'prompt')
  String get prompt;

  ExecuteRequest._();

  factory ExecuteRequest([void updates(ExecuteRequestBuilder b)]) = _$ExecuteRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ExecuteRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ExecuteRequest> get serializer => _$ExecuteRequestSerializer();
}

class _$ExecuteRequestSerializer implements PrimitiveSerializer<ExecuteRequest> {
  @override
  final Iterable<Type> types = const [ExecuteRequest, _$ExecuteRequest];

  @override
  final String wireName = r'ExecuteRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ExecuteRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'slug';
    yield serializers.serialize(
      object.slug,
      specifiedType: const FullType(String),
    );
    yield r'prompt';
    yield serializers.serialize(
      object.prompt,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    ExecuteRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ExecuteRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'slug':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.slug = valueDes;
          break;
        case r'prompt':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.prompt = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ExecuteRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ExecuteRequestBuilder();
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

