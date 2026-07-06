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
/// * [state] 
/// * [limit] 
@BuiltValue()
abstract class ListRequest implements Built<ListRequest, ListRequestBuilder> {
  @BuiltValueField(wireName: r'state')
  String? get state;

  @BuiltValueField(wireName: r'limit')
  int? get limit;

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
    if (object.state != null) {
      yield r'state';
      yield serializers.serialize(
        object.state,
        specifiedType: const FullType(String),
      );
    }
    if (object.limit != null) {
      yield r'limit';
      yield serializers.serialize(
        object.limit,
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
        case r'state':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.state = valueDes;
          break;
        case r'limit':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.limit = valueDes;
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

