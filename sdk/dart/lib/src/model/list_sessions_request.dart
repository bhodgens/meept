//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'list_sessions_request.g.dart';

/// ListSessionsRequest
///
/// Properties:
/// * [limitCommaOmitempty] 
@BuiltValue()
abstract class ListSessionsRequest implements Built<ListSessionsRequest, ListSessionsRequestBuilder> {
  @BuiltValueField(wireName: r'limit,omitempty')
  int? get limitCommaOmitempty;

  ListSessionsRequest._();

  factory ListSessionsRequest([void updates(ListSessionsRequestBuilder b)]) = _$ListSessionsRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ListSessionsRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ListSessionsRequest> get serializer => _$ListSessionsRequestSerializer();
}

class _$ListSessionsRequestSerializer implements PrimitiveSerializer<ListSessionsRequest> {
  @override
  final Iterable<Type> types = const [ListSessionsRequest, _$ListSessionsRequest];

  @override
  final String wireName = r'ListSessionsRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ListSessionsRequest object, {
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
    ListSessionsRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ListSessionsRequestBuilder result,
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
  ListSessionsRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ListSessionsRequestBuilder();
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

