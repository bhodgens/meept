//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_collection/built_collection.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'list_events_response.g.dart';

/// ListEventsResponse
///
/// Properties:
/// * [events] 
/// * [count] 
@BuiltValue()
abstract class ListEventsResponse implements Built<ListEventsResponse, ListEventsResponseBuilder> {
  @BuiltValueField(wireName: r'events')
  BuiltList<String>? get events;

  @BuiltValueField(wireName: r'count')
  int get count;

  ListEventsResponse._();

  factory ListEventsResponse([void updates(ListEventsResponseBuilder b)]) = _$ListEventsResponse;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ListEventsResponseBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ListEventsResponse> get serializer => _$ListEventsResponseSerializer();
}

class _$ListEventsResponseSerializer implements PrimitiveSerializer<ListEventsResponse> {
  @override
  final Iterable<Type> types = const [ListEventsResponse, _$ListEventsResponse];

  @override
  final String wireName = r'ListEventsResponse';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ListEventsResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'events';
    yield object.events == null ? null : serializers.serialize(
      object.events,
      specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
    );
    yield r'count';
    yield serializers.serialize(
      object.count,
      specifiedType: const FullType(int),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    ListEventsResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ListEventsResponseBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'events':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(BuiltList, [FullType(String)]),
          ) as BuiltList<String>?;
          if (valueDes == null) continue;
          result.events.replace(valueDes);
          break;
        case r'count':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.count = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ListEventsResponse deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ListEventsResponseBuilder();
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

