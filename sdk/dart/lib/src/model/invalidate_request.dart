//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'invalidate_request.g.dart';

/// InvalidateRequest
///
/// Properties:
/// * [pathCommaOmitempty] 
@BuiltValue()
abstract class InvalidateRequest implements Built<InvalidateRequest, InvalidateRequestBuilder> {
  @BuiltValueField(wireName: r'path,omitempty')
  String? get pathCommaOmitempty;

  InvalidateRequest._();

  factory InvalidateRequest([void updates(InvalidateRequestBuilder b)]) = _$InvalidateRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(InvalidateRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<InvalidateRequest> get serializer => _$InvalidateRequestSerializer();
}

class _$InvalidateRequestSerializer implements PrimitiveSerializer<InvalidateRequest> {
  @override
  final Iterable<Type> types = const [InvalidateRequest, _$InvalidateRequest];

  @override
  final String wireName = r'InvalidateRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    InvalidateRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.pathCommaOmitempty != null) {
      yield r'path,omitempty';
      yield serializers.serialize(
        object.pathCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    InvalidateRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required InvalidateRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'path,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.pathCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  InvalidateRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = InvalidateRequestBuilder();
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

