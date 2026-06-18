//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'list_branches_request.g.dart';

/// ListBranchesRequest
///
/// Properties:
/// * [id] 
@BuiltValue()
abstract class ListBranchesRequest implements Built<ListBranchesRequest, ListBranchesRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  ListBranchesRequest._();

  factory ListBranchesRequest([void updates(ListBranchesRequestBuilder b)]) = _$ListBranchesRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ListBranchesRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ListBranchesRequest> get serializer => _$ListBranchesRequestSerializer();
}

class _$ListBranchesRequestSerializer implements PrimitiveSerializer<ListBranchesRequest> {
  @override
  final Iterable<Type> types = const [ListBranchesRequest, _$ListBranchesRequest];

  @override
  final String wireName = r'ListBranchesRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ListBranchesRequest object, {
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
    ListBranchesRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ListBranchesRequestBuilder result,
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
  ListBranchesRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ListBranchesRequestBuilder();
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

