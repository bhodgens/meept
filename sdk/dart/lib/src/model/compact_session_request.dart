//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'compact_session_request.g.dart';

/// CompactSessionRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class CompactSessionRequest implements Built<CompactSessionRequest, CompactSessionRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  CompactSessionRequest._();

  factory CompactSessionRequest([void updates(CompactSessionRequestBuilder b)]) = _$CompactSessionRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CompactSessionRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CompactSessionRequest> get serializer => _$CompactSessionRequestSerializer();
}

class _$CompactSessionRequestSerializer implements PrimitiveSerializer<CompactSessionRequest> {
  @override
  final Iterable<Type> types = const [CompactSessionRequest, _$CompactSessionRequest];

  @override
  final String wireName = r'CompactSessionRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CompactSessionRequest object, {
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
    CompactSessionRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CompactSessionRequestBuilder result,
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
  CompactSessionRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CompactSessionRequestBuilder();
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

