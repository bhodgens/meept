//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_collection/built_collection.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'paginated_response.g.dart';

/// PaginatedResponse
///
/// Properties:
/// * [items] 
/// * [total] 
/// * [hasMore] 
/// * [nextOffsetCommaOmitempty] 
@BuiltValue()
abstract class PaginatedResponse implements Built<PaginatedResponse, PaginatedResponseBuilder> {
  @BuiltValueField(wireName: r'items')
  BuiltList<String>? get items;

  @BuiltValueField(wireName: r'total')
  int get total;

  @BuiltValueField(wireName: r'has_more')
  bool get hasMore;

  @BuiltValueField(wireName: r'next_offset,omitempty')
  int? get nextOffsetCommaOmitempty;

  PaginatedResponse._();

  factory PaginatedResponse([void updates(PaginatedResponseBuilder b)]) = _$PaginatedResponse;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(PaginatedResponseBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<PaginatedResponse> get serializer => _$PaginatedResponseSerializer();
}

class _$PaginatedResponseSerializer implements PrimitiveSerializer<PaginatedResponse> {
  @override
  final Iterable<Type> types = const [PaginatedResponse, _$PaginatedResponse];

  @override
  final String wireName = r'PaginatedResponse';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    PaginatedResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'items';
    yield object.items == null ? null : serializers.serialize(
      object.items,
      specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
    );
    yield r'total';
    yield serializers.serialize(
      object.total,
      specifiedType: const FullType(int),
    );
    yield r'has_more';
    yield serializers.serialize(
      object.hasMore,
      specifiedType: const FullType(bool),
    );
    if (object.nextOffsetCommaOmitempty != null) {
      yield r'next_offset,omitempty';
      yield serializers.serialize(
        object.nextOffsetCommaOmitempty,
        specifiedType: const FullType(int),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    PaginatedResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required PaginatedResponseBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'items':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
          ) as BuiltList<String>?;
          if (valueDes == null) continue;
          result.items.replace(valueDes);
          break;
        case r'total':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.total = valueDes;
          break;
        case r'has_more':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.hasMore = valueDes;
          break;
        case r'next_offset,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.nextOffsetCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  PaginatedResponse deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = PaginatedResponseBuilder();
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

