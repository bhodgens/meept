//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'get_tree_request.g.dart';

/// GetTreeRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class GetTreeRequest implements Built<GetTreeRequest, GetTreeRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  GetTreeRequest._();

  factory GetTreeRequest([void updates(GetTreeRequestBuilder b)]) = _$GetTreeRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(GetTreeRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<GetTreeRequest> get serializer => _$GetTreeRequestSerializer();
}

class _$GetTreeRequestSerializer implements PrimitiveSerializer<GetTreeRequest> {
  @override
  final Iterable<Type> types = const [GetTreeRequest, _$GetTreeRequest];

  @override
  final String wireName = r'GetTreeRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    GetTreeRequest object, {
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
    GetTreeRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required GetTreeRequestBuilder result,
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
  GetTreeRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = GetTreeRequestBuilder();
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

