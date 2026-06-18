//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'clear_cache_request.g.dart';

/// ClearCacheRequest
///
/// Properties:
/// * [prefixCommaOmitempty] 
@BuiltValue()
abstract class ClearCacheRequest implements Built<ClearCacheRequest, ClearCacheRequestBuilder> {
  @BuiltValueField(wireName: r'prefix,omitempty')
  String? get prefixCommaOmitempty;

  ClearCacheRequest._();

  factory ClearCacheRequest([void updates(ClearCacheRequestBuilder b)]) = _$ClearCacheRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ClearCacheRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ClearCacheRequest> get serializer => _$ClearCacheRequestSerializer();
}

class _$ClearCacheRequestSerializer implements PrimitiveSerializer<ClearCacheRequest> {
  @override
  final Iterable<Type> types = const [ClearCacheRequest, _$ClearCacheRequest];

  @override
  final String wireName = r'ClearCacheRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ClearCacheRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.prefixCommaOmitempty != null) {
      yield r'prefix,omitempty';
      yield serializers.serialize(
        object.prefixCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    ClearCacheRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ClearCacheRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'prefix,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.prefixCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ClearCacheRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ClearCacheRequestBuilder();
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

