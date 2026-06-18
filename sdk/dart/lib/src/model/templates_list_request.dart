//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'templates_list_request.g.dart';

/// TemplatesListRequest
///
/// Properties:
/// * [limitCommaOmitempty] 
@BuiltValue()
abstract class TemplatesListRequest implements Built<TemplatesListRequest, TemplatesListRequestBuilder> {
  @BuiltValueField(wireName: r'limit,omitempty')
  int? get limitCommaOmitempty;

  TemplatesListRequest._();

  factory TemplatesListRequest([void updates(TemplatesListRequestBuilder b)]) = _$TemplatesListRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(TemplatesListRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<TemplatesListRequest> get serializer => _$TemplatesListRequestSerializer();
}

class _$TemplatesListRequestSerializer implements PrimitiveSerializer<TemplatesListRequest> {
  @override
  final Iterable<Type> types = const [TemplatesListRequest, _$TemplatesListRequest];

  @override
  final String wireName = r'TemplatesListRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    TemplatesListRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.limitCommaOmitempty != null) {
      yield r'limit,omitempty';
      yield serializers.serialize(
        object.limitCommaOmitempty,
        specifiedType: const FullType(int),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    TemplatesListRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required TemplatesListRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'limit,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.limitCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  TemplatesListRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = TemplatesListRequestBuilder();
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

