//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'list_request.g.dart';

/// ListRequest
///
/// Properties:
/// * [stateCommaOmitempty] 
/// * [limitCommaOmitempty] 
@BuiltValue()
abstract class ListRequest implements Built<ListRequest, ListRequestBuilder> {
  @BuiltValueField(wireName: r'state,omitempty')
  String? get stateCommaOmitempty;

  @BuiltValueField(wireName: r'limit,omitempty')
  int? get limitCommaOmitempty;

  ListRequest._();

  factory ListRequest([void updates(ListRequestBuilder b)]) = _$ListRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ListRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ListRequest> get serializer => _$ListRequestSerializer();
}

class _$ListRequestSerializer implements PrimitiveSerializer<ListRequest> {
  @override
  final Iterable<Type> types = const [ListRequest, _$ListRequest];

  @override
  final String wireName = r'ListRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ListRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.stateCommaOmitempty != null) {
      yield r'state,omitempty';
      yield serializers.serialize(
        object.stateCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
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
    ListRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ListRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'state,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.stateCommaOmitempty = valueDes;
          break;
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
  ListRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ListRequestBuilder();
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

